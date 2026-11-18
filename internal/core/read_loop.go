package core

import (
	"io"
	"reflect"
	"time"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/aggregation"
	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/memorybroker"
	ptrn "github.com/dangduoc08/ginject/internal/pattern"
	"github.com/dangduoc08/ginject/internal/wsevent"
	"golang.org/x/net/websocket"
)

type WSPayloadType string

const (
	TypeConnected   WSPayloadType = "connected"
	TypeSubscribe   WSPayloadType = "subscribe"
	TypeUnsubscribe WSPayloadType = "unsubscribe"
	TypePublish     WSPayloadType = "publish"
	TypeEvent       WSPayloadType = "event"
	TypeAck         WSPayloadType = "ack"
	TypeError       WSPayloadType = "error"
	TypePing        WSPayloadType = "ping"
	TypePong        WSPayloadType = "pong"
)

type WSPayload struct {
	Message any           `json:"message"`
	Type    WSPayloadType `json:"type"`
	ID      string        `json:"id"`
	Topic   []string      `json:"topic,omitempty"`
}

func readLoop(conn *WSConnection, ws *WS) {
	for {
		var payload WSPayload
		if err := websocket.JSON.Receive(conn.Conn, &payload); err != nil {
			if err != io.EOF {
				ws.logger.Error("WSReadFailed", "error", err)
			}
			return
		}

		ws.connmgr.touch(conn.ID)

		switch payload.Type {
		case TypeSubscribe:
			handleSubscribe(conn, ws, payload)
		case TypeUnsubscribe:
			handleUnsubscribe(conn, ws.connmgr, payload)
		case TypePublish:
			handlePublish(conn, ws, payload)
		case TypePing:
			conn.TrySend(WSPayload{Type: TypePong})
		case TypePong:

		default:
			reply(conn, TypeError, payload.ID, "unsupported type: "+string(payload.Type))
		}
	}
}

func handleSubscribe(conn *WSConnection, ws *WS, payload WSPayload) {
	for _, topic := range payload.Topic {
		if ws.connmgr.isSubscribed(conn.ID, topic) {
			continue
		}

		item, pattern, ok := ws.eventMatcher.Match(topic)
		if !ok {
			replyException(conn, payload.ID, exception.TopicNotFoundException("no handler registered for topic: "+topic))
			return
		}
		var live *wsLive
		if item.Gate != nil {
			if live = openWSGate(item.Gate); live == nil {
				replyNotReady(conn, payload.ID, topic)
				return
			}
			item = live.item
		}

		c, ok := runWSMiddlewares(conn, ws, pattern, item, payload, live)
		ws.emitComplete(c, string(payload.Type), topic)
		ws.releaseCtx(c)
		if !ok {
			return
		}

		allowed := handlerFilter(topic, pattern)
		err := ws.connmgr.Subscribe(conn.ID, topic, func(m *memorybroker.Message) {
			if allowed != nil && !allowed.Match(m.Topic) {
				return
			}
			conn.TrySend(WSPayload{
				Type:    TypeEvent,
				Topic:   []string{m.Topic},
				Message: m.Payload,
			})
		})
		if err != nil {
			replyException(conn, payload.ID, exception.WSInternalErrorException(err.Error()))
			return
		}
	}
	reply(conn, TypeAck, payload.ID, "")
}

// handlerFilter keeps a subscription within its handler's pattern: a client topic such as `**`
// matches the handler `*` as a literal segment, but subscribes the broker to every topic.
func handlerFilter(topic, handlerPattern string) *ptrn.Pattern {
	if topic == handlerPattern {
		return nil
	}
	p := ptrn.NewPattern(handlerPattern)
	return &p
}

func handleUnsubscribe(conn *WSConnection, connmgr *WSConnmgr, payload WSPayload) {
	for _, topic := range payload.Topic {
		if err := connmgr.Unsubscribe(conn.ID, topic); err != nil {
			replyException(conn, payload.ID, exception.WSInternalErrorException(err.Error()))
			return
		}
	}
}

func handlePublish(conn *WSConnection, ws *WS, payload WSPayload) {
	for _, topic := range payload.Topic {
		item, pattern, ok := ws.eventMatcher.Match(topic)
		if !ok {
			replyException(conn, payload.ID, exception.TopicNotFoundException("no handler registered for topic: "+topic))
			return
		}
		var live *wsLive
		if item.Gate != nil {
			if live = openWSGate(item.Gate); live == nil {
				replyNotReady(conn, payload.ID, topic)
				return
			}
			item = live.item
		}

		if !ws.connmgr.isSubscribed(conn.ID, topic) {
			replyException(conn, payload.ID, exception.NotSubscribedException("must subscribe before publishing to: "+topic))
			return
		}

		if !dispatchWSEvent(conn, ws, pattern, topic, item, payload, live) {
			return
		}

		if err := (*ws.connmgr.Broker).Publish(topic, payload.Message); err != nil {
			replyException(conn, payload.ID, exception.WSInternalErrorException(err.Error()))
			return
		}
	}
	reply(conn, TypeAck, payload.ID, "")
}

func replyNotReady(conn *WSConnection, id, topic string) {
	replyException(conn, id, exception.ServiceUnavailableException("not ready: "+topic))
}

func runCatchChain(conn *WSConnection, ws *WS, c *ctx.WSContext, pattern string, payloadID string, rec any, live *wsLive) {
	ctx.WSSetSend(c, func(data any) {
		reply(conn, TypeError, payloadID, data)
	})

	if live != nil {
		if len(live.catchFns) > 0 {
			common.RunWSCatchChain(c, live.catchFns, rec)
			return
		}
	} else if catchFns, ok := ws.catchFnsByEvent[pattern]; ok {
		common.RunWSCatchChain(c, catchFns, rec)
		return
	}

	replyException(conn, payloadID, *common.NormalizeRecovered(rec))
}

func replyException(conn *WSConnection, id string, ex exception.Exception) {
	reply(conn, TypeError, id, ctx.Map{
		"code":    ex.Code(),
		"error":   ex.Error(),
		"message": ex.Message(),
	})
}

func runWSMiddlewares(conn *WSConnection, ws *WS, pattern string, item wsevent.WSEventItem, payload WSPayload, live *wsLive) (c *ctx.WSContext, ok bool) {
	c = ws.newCtx()

	defer func() {
		if rec := recover(); rec != nil {
			runCatchChain(conn, ws, c, pattern, payload.ID, rec, live)
			ok = false
		}
	}()

	ctx.WSInit(c, conn.Conn)
	messageMap, _ := payload.Message.(map[string]any)
	ctx.WSSetPayload(c, ctx.WSPayload(messageMap))

	isNext := true
	ctx.WSSetNext(c, func() { isNext = true })

	for _, middleware := range item.Middlewares {
		if isNext {
			isNext = false
			middleware(c)
		}
	}

	return c, isNext
}

func dispatchWSEvent(conn *WSConnection, ws *WS, pattern, topic string, item wsevent.WSEventItem, payload WSPayload, live *wsLive) (ok bool) {
	c, mwOK := runWSMiddlewares(conn, ws, pattern, item, payload, live)
	defer ws.releaseCtx(c)
	defer func() {
		ws.emitComplete(c, string(payload.Type), topic)
	}()

	if !mwOK {
		return false
	}

	defer func() {
		if rec := recover(); rec != nil {
			runCatchChain(conn, ws, c, pattern, payload.ID, rec, live)
			ok = false
		}
	}()

	var data []reflect.Value
	if live != nil {
		data = live.plan.invoke(c, ws.tracer)
	} else {
		data = ws.resolveAndCallHandler(pattern, item.Handler, c)
	}

	if aggregations := ctx.WSAggregations(c); len(aggregations) > 0 {
		var aggregatedData any
		totalAggregations := len(aggregations)

		for i := totalAggregations - 1; i >= 0; i-- {
			agg := aggregations[i]

			if !aggregation.IsMainHandlerCalled(agg) {
				for j := i; j >= 0; j-- {
					ws.emitPostInterceptor(c, aggregation.Name(aggregations[j]), 0)
				}
				reply(conn, TypeEvent, payload.ID, aggregation.InterceptorData(agg))
				return true
			}

			if i == totalAggregations-1 && len(data) > 0 {
				aggregatedData = data[len(data)-1].Interface()
			}
			aggregation.SetMainData(agg, aggregatedData)
			start := time.Now()
			aggregatedData = aggregation.Aggregate(agg)
			ws.emitPostInterceptor(c, aggregation.Name(agg), time.Since(start))
		}

		reply(conn, TypeEvent, payload.ID, aggregatedData)
		return true
	}

	if len(data) > 0 {
		reply(conn, TypeEvent, payload.ID, data[len(data)-1].Interface())
	}

	return true
}

func reply(conn *WSConnection, t WSPayloadType, id string, message any) {
	conn.TrySend(WSPayload{ID: id, Type: t, Message: message})
}
