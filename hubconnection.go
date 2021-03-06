package signalr

import (
	"bytes"
	"fmt"
	"github.com/go-kit/kit/log"
	"reflect"
	"sync/atomic"
)

type hubConnection interface {
	Start()
	IsConnected() bool
	Close(error string)
	GetConnectionID() string
	Receive() (interface{}, error)
	SendInvocation(target string, args ...interface{})
	StreamItem(id string, item interface{})
	Completion(id string, result interface{}, error string)
	Ping()
	Items() map[string]interface{}
}

func newHubConnection(connection Connection, protocol HubProtocol, info log.Logger, debug log.Logger) hubConnection {
	info = log.WithPrefix(info, "ts", log.DefaultTimestampUTC,
		"class", "HubConnection")
	debug = log.WithPrefix(debug, "ts", log.DefaultTimestampUTC,
		"class", "HubConnection",
		"conn", reflect.ValueOf(connection).Elem().Type(),
		"protocol", reflect.ValueOf(protocol).Elem().Type())
	return &defaultHubConnection{
		Protocol:   protocol,
		Connection: connection,
		items:      make(map[string]interface{}),
		info:       info,
		dbg:        debug,
	}
}

type defaultHubConnection struct {
	Protocol   HubProtocol
	Connected  int32
	Connection Connection
	items      map[string]interface{}
	info       log.Logger
	dbg        log.Logger
}

func (c *defaultHubConnection) Items() map[string]interface{} {
	return c.items
}

func (c *defaultHubConnection) Start() {
	atomic.CompareAndSwapInt32(&c.Connected, 0, 1)
}

func (c *defaultHubConnection) IsConnected() bool {
	return atomic.LoadInt32(&c.Connected) == 1
}

func (c *defaultHubConnection) Close(error string) {
	atomic.StoreInt32(&c.Connected, 0)

	var closeMessage = closeMessage{
		Type:           7,
		Error:          error,
		AllowReconnect: true,
	}
	c.writeMessage(closeMessage)
}

func (c *defaultHubConnection) GetConnectionID() string {
	return c.Connection.ConnectionID()
}

func (c *defaultHubConnection) SendInvocation(target string, args ...interface{}) {
	var invocationMessage = sendOnlyHubInvocationMessage{
		Type:      1,
		Target:    target,
		Arguments: args,
	}
	c.writeMessage(invocationMessage)
}

func (c *defaultHubConnection) Ping() {
	var pingMessage = hubMessage{
		Type: 6,
	}
	c.writeMessage(pingMessage)
}

func (c *defaultHubConnection) Receive() (interface{}, error) {
	var buf bytes.Buffer
	var data = make([]byte, 1<<12) // 4K
	var n int
	for {
		if message, complete, err := c.Protocol.ReadMessage(&buf); !complete {
			// Partial message, need more data
			// ReadMessage read data out of the buf, so its gone there: refill
			buf.Write(data[:n])
			if n, err = c.Connection.Read(data); err == nil {
				buf.Write(data[:n])
			} else {
				return nil, err
			}
		} else {
			return message, err
		}
	}
}

func (c *defaultHubConnection) Completion(id string, result interface{}, error string) {
	var completionMessage = completionMessage{
		Type:         3,
		InvocationID: id,
		Result:       result,
		Error:        error,
	}
	c.writeMessage(completionMessage)
}

func (c *defaultHubConnection) StreamItem(id string, item interface{}) {
	var streamItemMessage = streamItemMessage{
		Type:         2,
		InvocationID: id,
		Item:         item,
	}
	c.writeMessage(streamItemMessage)
}

func (c *defaultHubConnection) writeMessage(message interface{}) {
	if err := c.Protocol.WriteMessage(message, c.Connection); err != nil {
		_ = c.info.Log(evt, "send invocation", "error",
			fmt.Sprintf("cannot send message %v over connection %v: %v", message, c.GetConnectionID(), err))
	}
}
