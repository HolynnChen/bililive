package bililive

import (
	"sync"
)

type MsgProcessList struct {
	lock     *sync.Cond
	readNode *saveDataNode
	lastNode *saveDataNode
	pool     sync.Pool
}

type saveDataNode struct {
	next *saveDataNode
	data interface{}
}

func NewMsgProcessList() *MsgProcessList {
	initNode := &saveDataNode{}
	return &MsgProcessList{
		lock:     sync.NewCond(&sync.Mutex{}),
		readNode: initNode,
		lastNode: initNode,
		pool: sync.Pool{New: func() interface{} {
			return new(saveDataNode)
		}},
	}
}

func (l *MsgProcessList) Put(data interface{}) {
	l.lock.L.Lock()
	newNode := l.pool.Get().(*saveDataNode)
	newNode.data = data
	l.lastNode.next = newNode
	l.lastNode = newNode
	l.lock.L.Unlock()
	l.lock.Signal()
}

func (l *MsgProcessList) Get() interface{} {
	l.lock.L.Lock()
	for l.readNode.next == nil {
		l.lock.Wait()
	}
	data := l.readNode.next.data
	tmp := l.readNode
	l.readNode = l.readNode.next
	tmp.data = nil
	tmp.next = nil
	l.pool.Put(tmp)
	l.lock.L.Unlock()
	return data
}
