package utils

import (
	"container/list"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
)

type Request struct {
	Data   *RequestData
	Schema *RequestSchema
}

type RequestData struct {
	Method       *string
	Path         *string
	PathTemplate *string
	Params       map[string]string
	BodyBytes    []byte
	ContentType  *string
	Kind         *string
}

type RequestSchema struct {
	ApiSpec      *openapi3.Operation
	SharedParams openapi3.Parameters
}

type ResponseData struct {
	StatusCode int
	Body       []byte
}

type ResponseAnalyzeResult struct {
	Req  *Request
	Resp *ResponseData
	Time time.Time
}

type RequestQueue struct {
	l *list.List
}

func NewRequestQueue() *RequestQueue {
	return &RequestQueue{l: list.New()}
}

func (q *RequestQueue) Enqueue(v Request) {
	q.l.PushBack(v)
}

func (q *RequestQueue) Dequeue() (Request, bool) {
	e := q.l.Front()
	if e == nil {
		return Request{}, false
	}
	q.l.Remove(e)
	return e.Value.(Request), true
}

func (q *RequestQueue) GetRandRequest() (Request, bool) {
	if q.l.Len() == 0 {
		return Request{}, false
	}

	// Get a random element from the list
	index := R.IntN(q.l.Len())
	e := q.l.Front()
	for i := 0; i < index; i++ {
		e = e.Next()
	}
	if e == nil {
		return Request{}, false
	}
	return e.Value.(Request), true
}

func (q *RequestQueue) Len() int {
	return q.l.Len()
}
