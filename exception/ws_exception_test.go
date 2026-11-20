package exception

import (
	"testing"

	"go.owlkei.com/owlkei/internal/test"
)

func TestWSExceptionHelpers(t *testing.T) {
	cases := []struct {
		fn     func(string, ...any) Exception
		status int
		text   string
	}{
		{GoingAwayException, 1001, "Going Away"},
		{ProtocolErrorException, 1002, "Protocol Error"},
		{UnsupportedDataException, 1003, "Unsupported Data"},
		{InvalidPayloadException, 1007, "Invalid Frame Payload Data"},
		{PolicyViolationException, 1008, "Policy Violation"},
		{MessageTooBigException, 1009, "Message Too Big"},
		{WSInternalErrorException, 1011, "Internal Error"},
		{NotSubscribedException, 4001, "Not Subscribed"},
		{TopicNotFoundException, 4004, "Topic Not Found"},
	}
	for _, c := range cases {
		e := c.fn("body")
		if e.Code() != c.status {
			t.Error(test.DiffMessage(e.Code(), c.status, "WS close status helper code"))
		}
		if e.StatusText() != c.text {
			t.Error(test.DiffMessage(e.StatusText(), c.text, "WS close status helper text"))
		}
	}
}

func TestWSCloseStatusText_ReservedCodeHasNoText(t *testing.T) {
	e := NewException("body", 1006)
	if text := e.StatusText(); text != "" {
		t.Error(test.DiffMessage(text, "", "reserved WS close code 1006 must resolve to no text"))
	}
}
