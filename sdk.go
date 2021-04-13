package bililive

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/goccy/go-json"
)

const (
	roomInitURL                    string = "https://api.live.bilibili.com/room/v1/Room/room_init?id=%d"
	roomConfigURL                  string = "https://api.live.bilibili.com/room/v1/Danmu/getConf?room_id=%d"
	roomConfigURL2                 string = "https://api.live.bilibili.com/xlive/web-room/v1/index/getDanmuInfo?id=%d"
	WS_OP_HEARTBEAT                int32  = 2
	WS_OP_HEARTBEAT_REPLY          int32  = 3
	WS_OP_MESSAGE                  int32  = 5
	WS_OP_USER_AUTHENTICATION      int32  = 7
	WS_OP_CONNECT_SUCCESS          int32  = 8
	WS_PACKAGE_HEADER_TOTAL_LENGTH int32  = 16
	//WS_PACKAGE_OFFSET                int32 = 0
	//WS_HEADER_OFFSET                 int32 = 4
	//WS_VERSION_OFFSET                int32 = 6
	//WS_OPERATION_OFFSET              int32 = 8
	//WS_SEQUENCE_OFFSET               int32 = 12
	//WS_BODY_PROTOCOL_VERSION_NORMAL  int32 = 0
	WS_BODY_PROTOCOL_VERSION_DEFLATE int16 = 2
	WS_HEADER_DEFAULT_VERSION        int16 = 1
	//WS_HEADER_DEFAULT_OPERATION      int32 = 1
	WS_HEADER_DEFAULT_SEQUENCE int32 = 1
	WS_AUTH_OK                 int32 = 0
	WS_AUTH_TOKEN_ERROR        int32 = -101
)

const (
	DEBUG_ALL            = 1<<64 - 1
	DEBUG_UNKNOW_MSG     = 1
	DEBUG_ERROR          = 1 << 1 // err!=nil
	DEBUG_CONNECT_SUCESS = 1 << 2
	DEBUG_CREATE_CONNECT = 1 << 3
)

var (
	cmdPool = sync.Pool{New: func() interface{} { return new(cmdModel) }}
	msgPool = sync.Pool{New: func() interface{} { return new(MsgModel) }}
)

func (m *cmdModel) reset() {
	m.CMD = ""
	//内部会优化为mapclear
	for k := range m.Data {
		delete(m.Data, k)
	}
	m.Info = m.Info[:0]
}

func (m *MsgModel) reset() {
	m.UserID = 0
	m.UserName = ""
	m.UserLevel = 0
	m.IsAdmin = false
	m.MedalName = ""
	m.MedalUpName = ""
	m.MedalRoomID = 0
	m.MedalLevel = 0
	m.Content = ""
	m.Timestamp = 0
	m.CT = ""
}

// Start 开始接收
func (live *Live) Start(ctx context.Context) {
	live.ctx = ctx

	rand.Seed(time.Now().Unix())
	if live.AnalysisRoutineNum <= 0 {
		live.AnalysisRoutineNum = 1
	}

	live.room = make(map[int]*liveRoom)
	live.chSocketMessage = NewMsgProcessList()
	live.chOperation = NewMsgProcessList()
	if live.StormFilter && live.ReceiveMsg != nil {
		live.storming = make(map[int]bool)
		live.stormContent = make(map[int]map[int64]string)
	}

	live.wg = sync.WaitGroup{}

	for i := 0; i < live.AnalysisRoutineNum; i++ {
		live.wg.Add(1)
		go func() {
			defer live.wg.Done()
			live.analysis(ctx)
		}()
	}

	live.wg.Add(1)
	go func() {
		defer live.wg.Done()
		live.split(ctx)
	}()
}

func (live *Live) Wait() {
	live.wg.Wait()
}

// Join 添加房间
func (live *Live) Join(roomIDs ...int) error {
	if len(roomIDs) == 0 {
		return errors.New("没有要添加的房间")
	}

	for _, roomID := range roomIDs {
		if _, exist := live.room[roomID]; exist {
			return fmt.Errorf("房间 %d 已存在", roomID)
		}
	}
	for _, roomID := range roomIDs {
		nextCtx, cancel := context.WithCancel(live.ctx)

		room := &liveRoom{
			roomID: roomID,
			cancel: cancel,
			debug:  live.Debug,
		}
		live.room[roomID] = room
		room.enter()
		go room.heartBeat(nextCtx)
		live.stormContent[roomID] = make(map[int64]string)
		go room.receive(nextCtx, live.chSocketMessage)
	}
	return nil
}

// Remove 移出房间
func (live *Live) Remove(roomIDs ...int) error {
	if len(roomIDs) == 0 {
		return errors.New("没有要移出的房间")
	}

	for _, roomID := range roomIDs {
		if room, exist := live.room[roomID]; exist {
			room.cancel()
			delete(live.room, roomID)
		}
	}
	return nil
}

// 拆分数据
func (live *Live) split(ctx context.Context) {
	var (
		message            *socketMessage
		head               messageHeader
		headerBufferReader *bytes.Reader
		payloadBuffer      []byte
	)
	for {
		message = live.chSocketMessage.Get().(*socketMessage)
		for len(message.body) > 0 {
			select {
			case <-ctx.Done():
				return
			default:
			}

			headerBufferReader = bytes.NewReader(message.body[:WS_PACKAGE_HEADER_TOTAL_LENGTH])
			_ = binary.Read(headerBufferReader, binary.BigEndian, &head)
			payloadBuffer = message.body[WS_PACKAGE_HEADER_TOTAL_LENGTH:head.Length]
			message.body = message.body[head.Length:]

			if head.Length == WS_PACKAGE_HEADER_TOTAL_LENGTH {
				continue
			}

			if head.ProtocolVersion == WS_BODY_PROTOCOL_VERSION_DEFLATE {
				message.body = doZlibUnCompress(payloadBuffer)
				continue
			}
			if live.Debug&DEBUG_ERROR == 1 {
				log.Println(string(payloadBuffer))
			}
			live.chOperation.Put(&operateInfo{RoomID: message.roomID, Operation: head.Operation, Buffer: payloadBuffer})
		}
	}
}

// 分析接收到的数据
func (live *Live) analysis(ctx context.Context) {
analysis:
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		buffer := live.chOperation.Get().(*operateInfo)
		switch buffer.Operation {
		case WS_OP_HEARTBEAT_REPLY:
			if live.ReceivePopularValue != nil {
				m := binary.BigEndian.Uint32(buffer.Buffer)
				live.ReceivePopularValue(buffer.RoomID, m)
			}
		case WS_OP_CONNECT_SUCCESS:
			if live.Debug&DEBUG_CONNECT_SUCESS == 1 {
				log.Println("CONNECT_SUCCESS", string(buffer.Buffer))
			}
		case WS_OP_MESSAGE:
			result := cmdPool.Get().(*cmdModel)
			err := json.Unmarshal(buffer.Buffer, &result)
			if err != nil {
				if live.Debug&DEBUG_ERROR == 1 {
					log.Println(err)
					log.Println(string(buffer.Buffer))
				}
				continue
			}
			temp, err := json.Marshal(result.Data)
			if err != nil {
				if live.Debug&DEBUG_ERROR == 1 {
					log.Println(err)
				}
				continue
			}
			switch result.CMD {
			case "LIVE": // 直播开始
				if live.Live != nil {
					live.Live(buffer.RoomID)
				}
			case "CLOSE": // 关闭
				fallthrough
			case "PREPARING": // 准备
				fallthrough
			case "END": // 结束
				if live.End != nil {
					live.End(buffer.RoomID)
				}
			case "SYS_MSG": // 系统消息
				if live.SysMessage != nil {
					m := &SysMsgModel{}
					_ = json.Unmarshal(buffer.Buffer, m)
					live.SysMessage(buffer.RoomID, m)
				}
			case "ROOM_CHANGE": // 房间信息变更
				if live.RoomChange != nil {
					m := &RoomChangeModel{}
					_ = json.Unmarshal(temp, m)
					live.RoomChange(buffer.RoomID, m)
				}
			case "WELCOME": // 用户进入
				if live.UserEnter != nil {
					m := &UserEnterModel{}
					_ = json.Unmarshal(temp, m)
					live.UserEnter(buffer.RoomID, m)
				}
			case "WELCOME_GUARD": // 舰长进入
				if live.GuardEnter != nil {
					m := &GuardEnterModel{}
					_ = json.Unmarshal(temp, m)
					live.GuardEnter(buffer.RoomID, m)
				}
			case "DANMU_MSG": // 弹幕
				if live.ReceiveMsg != nil {
					msgContent := result.Info[1].(string)

					if live.LotteryDanmuFilter {
						markInfo := result.Info[0].([]interface{})
						if markInfo[9].(float64) != 0 {
							//log.Printf("过滤抽奖弹幕：%+v\n", result)
							continue analysis
						}
					}
					if live.StormFilter && live.storming[buffer.RoomID] {
						for _, value := range live.stormContent[buffer.RoomID] {
							if msgContent == value {
								//log.Println("过滤弹幕：", value)
								continue analysis
							}
						}
					}

					userInfo := result.Info[2].([]interface{})
					medalInfo := result.Info[3].([]interface{})
					m := msgPool.Get().(*MsgModel)
					m.UserID = int64(userInfo[0].(float64))
					m.UserName = userInfo[1].(string)
					m.IsAdmin = userInfo[2].(float64) > 0
					m.UserLevel = int(result.Info[4].([]interface{})[0].(float64))
					m.Content = msgContent
					m.CT = result.Info[9].(map[string]interface{})["ct"].(string)
					m.Timestamp = int64(result.Info[9].(map[string]interface{})["ts"].(float64))
					if len(medalInfo) >= 4 {
						m.MedalLevel = int(medalInfo[0].(float64))
						m.MedalName = medalInfo[1].(string)
						m.MedalUpName = medalInfo[2].(string)
						m.MedalRoomID = int64(medalInfo[3].(float64))
					}
					live.ReceiveMsg(buffer.RoomID, m)
					m.reset()
					msgPool.Put(m)
				}
			case "SEND_GIFT": // 礼物通知
				if live.ReceiveGift != nil {
					m := &GiftModel{}
					_ = json.Unmarshal(temp, m)
					live.ReceiveGift(buffer.RoomID, m)
				}
			case "COMBO_SEND": // 连击
				if live.GiftComboSend != nil {
					m := &ComboSendModel{}
					_ = json.Unmarshal(temp, m)
					live.GiftComboSend(buffer.RoomID, m)
				}
			case "COMBO_END": // 连击结束
				if live.GiftComboEnd != nil {
					m := &ComboEndModel{}
					_ = json.Unmarshal(temp, m)
					live.GiftComboEnd(buffer.RoomID, m)
				}
			case "GUARD_BUY": // 上船
				if live.GuardBuy != nil {
					m := &GuardBuyModel{}
					_ = json.Unmarshal(temp, m)
					live.GuardBuy(buffer.RoomID, m)
				}
			case "ROOM_REAL_TIME_MESSAGE_UPDATE": // 粉丝数更新
				if live.FansUpdate != nil {
					m := &FansUpdateModel{}
					_ = json.Unmarshal(temp, m)
					live.FansUpdate(buffer.RoomID, m)
				}
			case "ROOM_RANK": // 小时榜
				if live.RoomRank != nil {
					m := &RankModel{}
					_ = json.Unmarshal(temp, m)
					live.RoomRank(buffer.RoomID, m)
				}
			case "SPECIAL_GIFT": // 特殊礼物
				m := &SpecialGiftModel{}
				_ = json.Unmarshal(temp, m)
				if m.Storm.Action == "start" {
					m.Storm.ID, _ = strconv.ParseInt(m.Storm.TempID.(string), 10, 64)
				}
				if m.Storm.Action == "end" {
					m.Storm.ID = int64(m.Storm.TempID.(float64))
				}
				if live.StormFilter && live.ReceiveMsg != nil {
					if m.Storm.Action == "start" {
						live.storming[buffer.RoomID] = true
						live.stormContent[buffer.RoomID][m.Storm.ID] = m.Storm.Content
						//log.Println("添加过滤弹幕：", m.Storm.ID, m.Storm.Content)
					}
					if m.Storm.Action == "end" {
						delete(live.stormContent[buffer.RoomID], m.Storm.ID)
						live.storming[buffer.RoomID] = len(live.stormContent) > 0
						//log.Println("移除过滤弹幕：", m.Storm.ID, live.storming)
					}
				}
				if live.SpecialGift != nil {
					live.SpecialGift(buffer.RoomID, m)
				}
			case "SUPER_CHAT_MESSAGE": // 醒目留言
				if live.SuperChatMessage != nil {
					m := &SuperChatMessageModel{}
					_ = json.Unmarshal(temp, m)
					live.SuperChatMessage(buffer.RoomID, m)
				}
			case "SUPER_CHAT_MESSAGE_JPN":
				if live.Debug&DEBUG_UNKNOW_MSG == 1 {
					log.Println(string(buffer.Buffer))
				}
			case "SYS_GIFT": // 系统礼物
				fallthrough
			case "BLOCK": // 未知
				fallthrough
			case "ROUND": // 未知
				fallthrough
			case "REFRESH": // 刷新
				fallthrough
			case "ACTIVITY_BANNER_UPDATE_V2": //
				fallthrough
			case "ANCHOR_LOT_CHECKSTATUS": //
				fallthrough
			case "GUARD_MSG": // 舰长信息
				fallthrough
			case "NOTICE_MSG": // 通知信息
				fallthrough
			case "GUARD_LOTTERY_START": // 舰长抽奖开始
				fallthrough
			case "USER_TOAST_MSG": // 用户通知消息
				fallthrough
			case "ENTRY_EFFECT": // 进入效果
				fallthrough
			case "WISH_BOTTLE": // 许愿瓶
				fallthrough
			case "ROOM_BLOCK_MSG": // 房间封禁信息
				fallthrough
			case "WEEK_STAR_CLOCK":
				fallthrough
			case "INTERACT_WORD": // 互动信息
				fallthrough
			case "LIVE_INTERACTIVE_GAME":
				fallthrough
			case "xxx": // 上述不处理直接返回
				continue
			default:
				if live.Debug&DEBUG_UNKNOW_MSG == 1 { //把没注明的信息打出来
					log.Println(string(buffer.Buffer))
				}
			}
			result.reset()
			cmdPool.Put(result)
		default:
			break
		}
	}
}

func (room *liveRoom) findServer() error {
	// resRoom, err := httpSend(fmt.Sprintf(roomInitURL, room.roomID))
	// if err != nil {
	// 	return err
	// }

	// roomInfo := roomInfoResult{}
	// _ = json.Unmarshal(resRoom, &roomInfo)
	// if roomInfo.Code != 0 {
	// 	return errors.New("房间不正确")
	// }
	// room.realRoomID = roomInfo.Data.RoomID
	room.realRoomID = room.roomID // 强制要求输入为完整房间号
	resDanmuConfig, err := httpSend(fmt.Sprintf(roomConfigURL, room.realRoomID))
	if err != nil {
		return err
	}

	danmuConfig := danmuConfigResult{}
	err = json.Unmarshal(resDanmuConfig, &danmuConfig)
	if err != nil {
		return err
	}
	if danmuConfig.Data == nil {
		return errors.New("findServer no data")
	}
	room.server = danmuConfig.Data.Host
	room.port = danmuConfig.Data.Port
	room.hostServerList = danmuConfig.Data.HostServerList
	room.token = danmuConfig.Data.Token
	room.currentServerIndex = 0
	return nil
}

func (room *liveRoom) findServer2() error {
	room.realRoomID = room.roomID //轻质要求输入为完整房间号
	resDanmuConfig, err := httpSend(fmt.Sprintf(roomConfigURL2, room.realRoomID))
	if err != nil {
		return err
	}

	danmuConfig := danmuConfigResult2{}
	err = json.Unmarshal(resDanmuConfig, &danmuConfig)
	if err != nil {
		return err
	}
	if danmuConfig.Data == nil {
		return errors.New("findServer no data")
	}
	room.hostServerList = danmuConfig.Data.HostList
	room.token = danmuConfig.Data.Token
	room.currentServerIndex = 0
	return nil
}

func (room *liveRoom) createConnect() {
	for {
		if room.hostServerList == nil || len(room.hostServerList) == room.currentServerIndex {
			for {
				var err error
				if rand.Intn(2) == 0 {
					err = room.findServer()
				} else {
					err = room.findServer2()
				}
				if err != nil {
					log.Println("find server err:", err)
					time.Sleep(500 * time.Millisecond)
					continue
				}
				break
			}
		}

		counter := 0
		for {
			if room.debug&DEBUG_CREATE_CONNECT == 1 {
				log.Println("尝试创建连接：", room.hostServerList[room.currentServerIndex].Host, room.hostServerList[room.currentServerIndex].Port)
			}
			conn, err := connect(room.hostServerList[room.currentServerIndex].Host, room.hostServerList[room.currentServerIndex].Port)
			if err != nil {
				log.Println("connect err:", err)
				if counter == 3 {
					room.currentServerIndex++
					break
				}
				time.Sleep(1 * time.Second)
				counter++
				continue
			}
			room.conn = conn
			if room.debug&DEBUG_CREATE_CONNECT == 1 {
				log.Println("连接创建成功：", room.hostServerList[room.currentServerIndex].Host, room.hostServerList[room.currentServerIndex].Port)
			}
			room.currentServerIndex++
			return
		}
	}
}

func (room *liveRoom) enter() {
	room.createConnect()

	enterInfo := &enterInfo{
		RoomID:    room.realRoomID,
		UserID:    9999999999 + rand.Int63(),
		ProtoVer:  2,
		Platform:  "web",
		ClientVer: "1.10.6",
		Type:      2,
		Key:       room.token,
	}

	payload, err := json.Marshal(enterInfo)
	if err != nil {
		log.Panic(err)
	}
	room.sendData(WS_OP_USER_AUTHENTICATION, payload)
}

// 心跳
func (room *liveRoom) heartBeat(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		room.sendData(WS_OP_HEARTBEAT, []byte{})
		time.Sleep(30 * time.Second)
	}
}

// 接收消息
func (room *liveRoom) receive(ctx context.Context, chSocketMessage *MsgProcessList) {
	// 包头总长16个字节
	headerBuffer := make([]byte, WS_PACKAGE_HEADER_TOTAL_LENGTH)
	// headerBufferReader
	var headerBufferReader *bytes.Reader
	// 包体
	var messageBody []byte
	counter := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, err := io.ReadFull(room.conn, headerBuffer)
		if err != nil {
			if counter >= 10 {
				log.Panic(err)
			}
			if err != io.EOF {
				log.Println("header read err:", err)
			}
			room.enter()
			counter++
			continue
		}

		var head messageHeader
		headerBufferReader = bytes.NewReader(headerBuffer)
		_ = binary.Read(headerBufferReader, binary.BigEndian, &head)

		if head.Length < WS_PACKAGE_HEADER_TOTAL_LENGTH {
			if counter >= 10 {
				log.Println("***************协议失败***************")
				log.Println("数据包长度:", head.Length)
				log.Panic("数据包长度不正确")
			}
			room.enter()
			counter++
			continue
		}

		payloadBuffer := make([]byte, head.Length-WS_PACKAGE_HEADER_TOTAL_LENGTH)
		_, err = io.ReadFull(room.conn, payloadBuffer)
		if err != nil {
			if counter >= 10 {
				log.Panic(err)
			}
			if err != io.EOF {
				log.Println("payload read err:", err)
			}
			room.enter()
			counter++
			continue
		}

		messageBody = append(headerBuffer, payloadBuffer...)

		chSocketMessage.Put(&socketMessage{
			roomID: room.roomID,
			body:   messageBody,
		})
		counter = 0
	}
}

// 发送数据
func (room *liveRoom) sendData(operation int32, payload []byte) {

	b := bytes.NewBuffer([]byte{})
	head := messageHeader{
		Length:          int32(len(payload)) + WS_PACKAGE_HEADER_TOTAL_LENGTH,
		HeaderLength:    int16(WS_PACKAGE_HEADER_TOTAL_LENGTH),
		ProtocolVersion: WS_HEADER_DEFAULT_VERSION,
		Operation:       operation,
		SequenceID:      WS_HEADER_DEFAULT_SEQUENCE,
	}
	err := binary.Write(b, binary.BigEndian, head)
	if err != nil {
		log.Println(err)
	}

	err = binary.Write(b, binary.LittleEndian, payload)
	if err != nil {
		log.Println(err)
	}

	_, err = room.conn.Write(b.Bytes())
	if err != nil {
		log.Println(err)
	}
}

func connect(host string, port int) (*net.TCPConn, error) {
	tcpAddr, err := net.ResolveTCPAddr("tcp4", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return nil, err
	}
	return net.DialTCP("tcp", nil, tcpAddr)
}

var zlibReaderPool sync.Pool //缓存zlibreader

// 进行zlib解压缩
func doZlibUnCompress(compressSrc []byte) []byte {
	b := bytes.NewReader(compressSrc)
	var out bytes.Buffer
	var err error
	z := zlibReaderPool.Get()
	if z != nil {
		err = z.(zlib.Resetter).Reset(b, nil)
	} else {
		z, err = zlib.NewReader(b)
	}
	if err != nil {
		log.Panic("zlib", err)
	}
	defer zlibReaderPool.Put(z)
	rc := z.(io.ReadCloser)
	defer rc.Close()
	_, err = io.Copy(&out, rc)
	if err != nil {
		log.Println("zlib copy", err)
	}
	return out.Bytes()
}
