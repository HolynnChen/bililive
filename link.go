package bililive

import (
	"sync"
)

type MsgProcessList struct {
	readlock  *sync.Cond
	writelock *sync.Mutex
	readNode  *saveDataNode
	lastNode  *saveDataNode
	pool      sync.Pool
}

type saveDataNode struct {
	next *saveDataNode
	data interface{}
}

func NewMsgProcessList() *MsgProcessList {
	initNode := &saveDataNode{}
	return &MsgProcessList{
		readlock:  sync.NewCond(&sync.Mutex{}),
		writelock: &sync.Mutex{},
		readNode:  initNode,
		lastNode:  initNode,
		pool: sync.Pool{New: func() interface{} {
			return new(saveDataNode)
		}},
	}
}

func (l *MsgProcessList) Put(data interface{}) {
	l.writelock.Lock()
	newNode := l.pool.Get().(*saveDataNode)
	newNode.data = data
	l.lastNode.next = newNode
	l.lastNode = newNode
	l.writelock.Unlock()
	l.readlock.Signal()
}

func (l *MsgProcessList) Get() interface{} {
	l.readlock.L.Lock()
	for l.readNode.next == nil {
		l.readlock.Wait()
	}
	data := l.readNode.next.data
	tmp := l.readNode
	l.readNode = l.readNode.next
	tmp.data = nil
	tmp.next = nil
	l.pool.Put(tmp)
	l.readlock.L.Unlock()
	return data
}
