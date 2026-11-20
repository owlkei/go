package common

import (
	"testing"

	"go.owlkei.com/owlkei/internal/test"
)

func TestPipeableConstants(t *testing.T) {
	cases := []struct{ got, want string }{
		{ContextPipeableKey, "context"},
		{BodyPipeableKey, "body"},
		{FormPipeableKey, "form"},
		{QueryPipeableKey, "query"},
		{HeaderPipeableKey, "header"},
		{ParamPipeableKey, "param"},
		{FilePipeableKey, "file"},
		{WSPayloadPipeableKey, "wsPayload"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Error(test.DiffMessage(c.got, c.want, "pipeable constant"))
		}
	}
}

func TestArgumentMetadata_Fields(t *testing.T) {
	m := ArgumentMetadata{
		ContextType: "http",
		ParamType:   BodyPipeableKey,
	}
	if m.ContextType != "http" {
		t.Error(test.DiffMessage(m.ContextType, "http", "ContextType"))
	}
	if m.ParamType != BodyPipeableKey {
		t.Error(test.DiffMessage(m.ParamType, BodyPipeableKey, "ParamType"))
	}
}

func TestArgumentMetadata_Zero(t *testing.T) {
	var m ArgumentMetadata
	if m.ContextType != "" {
		t.Error(test.DiffMessage(m.ContextType, "", "zero ContextType"))
	}
	if m.ParamType != "" {
		t.Error(test.DiffMessage(m.ParamType, "", "zero ParamType"))
	}
}

func TestArgumentMetadata_ContextTypeConstants(t *testing.T) {
	if ContextTypeHTTP != "http" {
		t.Error(test.DiffMessage(ContextTypeHTTP, "http", "Q3: ContextTypeHTTP"))
	}
	if ContextTypeWS != "ws" {
		t.Error(test.DiffMessage(ContextTypeWS, "ws", "Q3: ContextTypeWS"))
	}
	m := ArgumentMetadata{ContextType: ContextTypeWS, ParamType: WSPayloadPipeableKey}
	if m.ContextType != "ws" {
		t.Error(test.DiffMessage(m.ContextType, "ws", "Q3: the constant fills ContextType"))
	}
}
