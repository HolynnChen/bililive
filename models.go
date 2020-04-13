package bililive

import "net"

// LiveRoom 直播间
type LiveRoom struct {
	RoomID              int                    // 房间ID（兼容短ID）
	RoomInfo            func(*RoomDetail)      // 房间信息
	ReceiveMsg          func(*MsgModel)        // 接收消息方法
	ReceiveGift         func(*GiftModel)       // 接收礼物方法
	ReceivePopularValue func(uint32)           // 接收人气值方法
	UserEnter           func(*UserEnterModel)  // 用户进入方法
	GuardEnter          func(*GuardEnterModel) // 舰长进入方法
	GiftComboSend       func(*ComboSendModel)  // 礼物连击方法
	GiftComboEnd        func(*ComboEndModel)   // 礼物连击结束方法
	GuardBuy            func(*GuardBuyModel)   // 上船
	FansUpdate          func(*FansUpdateModel) // 粉丝数更新
	RoomRank            func(*RankModel)       // 小时榜

	chRoomDetail    chan *RoomDetail
	chBuffer        chan *bufferInfo
	chMsg           chan *MsgModel
	chGift          chan *GiftModel
	chPopularValue  chan uint32
	chUserEnter     chan *UserEnterModel
	chGuardEnter    chan *GuardEnterModel
	chGiftComboSend chan *ComboSendModel
	chGiftComboEnd  chan *ComboEndModel
	chGuardBuy      chan *GuardBuyModel
	chFansUpdate    chan *FansUpdateModel
	chRank          chan *RankModel

	server string // 地址
	port   int    // 端口
	token  string // key
	conn   *net.TCPConn
}

type messageHeader struct {
	Length          int32
	HeaderLength    int16
	ProtocolVersion int16
	Operation       int32
	SequenceID      int32
}

type bufferInfo struct {
	Operation int32
	Buffer    []byte
}

// 进入房间信息
type enterInfo struct {
	RoomID    int    `json:"roomid"`
	UserID    uint64 `json:"uid"`
	ProtoVer  int    `json:"protover"`
	Platform  string `json:"platform"`
	ClientVer string `json:"clientver"`
	Type      int    `json:"type"`
	Key       string `json:"key"`
}

// 房间信息
type roomInfoResult struct {
	Code int           `json:"code"`
	Data *roomInfoData `json:"data"`
}

type roomDetailResult struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		RoomInfo *RoomDetail `json:"room_info"`
	} `json:"data"`
}

type RoomDetail struct {
	RoomID         int    `json:"room_id"`
	ShortID        int    `json:"short_id"`
	LiveStatus     int    `json:"live_status"`
	LiveStartTime  int64  `json:"live_start_time"`
	Title          string `json:"title"`
	AreaName       string `json:"area_name"`
	ParentAreaName string `json:"parent_area_name"`
}

// 房间数据
type roomInfoData struct {
	RoomID int `json:"room_id"`
}

// 弹幕信息
type danmuConfigResult struct {
	Data *danmuData `json:"data"`
}

type danmuData struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	HostServerList []struct {
		Host    string
		Port    int `json:"port"`
		WssPort int `json:"wss_port"`
		WsPort  int `json:"ws_port"`
	} `json:"host_server_list"`
	Token string `json:"token"`
}

// 命令模型
type cmdModel struct {
	CMD  string                 `json:"cmd"`
	Info []interface{}          `json:"info"`
	Data map[string]interface{} `json:"data"`
}

// UserEnterModel 用户进入模型
type UserEnterModel struct {
	UserID   int    `json:"uid"`
	UserName string `json:"uname"`
	IsAdmin  bool   `json:"is_admin"`
	VIP      int    `json:"vip"`
	SVIP     int    `json:"svip"`
}

// GuardEnterModel 舰长进入模型
type GuardEnterModel struct {
	UserID     int    `json:"uid"`
	UserName   string `json:"username"`
	GuardLevel int    `json:"guard_level"`
}

// GiftModel 礼物模型
type GiftModel struct {
	GiftName string `json:"giftName"`       // 礼物名称
	Num      int    `json:"num"`            // 数量
	UserName string `json:"uname"`          // 用户名称
	GiftID   int    `json:"giftId"`         // 礼物ID
	Price    int    `json:"price"`          // 价格
	CoinType string `json:"coin_type"`      // 硬币类型
	FaceURL  string `json:"face"`           // 头像url
	Combo    int    `json:"super_gift_num"` // 连击
}

// MsgModel 消息
type MsgModel struct {
	UserID   string // 用户ID
	UserName string // 用户昵称
	Content  string // 内容
}

// ComboSendModel 连击模型
type ComboSendModel struct {
	UserName string `json:"uname"`     // 用户名称
	GiftName string `json:"gift_name"` // 礼物名称
	GiftID   int    `json:"gift_id"`   // 礼物ID
	ComboNum int    `json:"combo_num"` // 连击数量
}

// ComboEndModel 连击结束模型
type ComboEndModel struct {
	GiftName   string `json:"gift_name"`   // 礼物名称
	ComboNum   int    `json:"combo_num"`   // 连击数量
	UserName   string `json:"uname"`       // 用户名称
	GiftID     int    `json:"gift_id"`     // 礼物ID
	Price      int    `json:"price"`       // 价格
	GuardLevel int    `json:"guard_level"` // 舰长等级
}

// GuardBuyModel 上船模型
type GuardBuyModel struct {
	GiftName   string `json:"gift_name"`   // 礼物名称
	Num        int    `json:"num"`         // 数量
	UserID     int    `json:"uid"`         // 用户ID
	UserName   string `json:"username"`    // 用户名称
	GiftID     int    `json:"gift_id"`     // 礼物ID
	Price      int    `json:"price"`       // 价格
	GuardLevel int    `json:"guard_level"` // 舰长等级
}

// FansUpdateModel 粉丝更新模型
type FansUpdateModel struct {
	RoomID    int `json:"roomid"`
	Fans      int `json:"fans"`
	RedNotice int `json:"red_notice"`
}

// RankModel 小时榜模型
type RankModel struct {
	RoomID    int    `json:"roomid"`
	RankDesk  string `json:"rank_desk"`
	Timestamp int64  `json:"timestamp"`
}
