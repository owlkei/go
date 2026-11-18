package devtool

import (
	"context"
	"encoding/json"
)

const PageKind = "page"

type LayerScope int32

const (
	LayerScope_UNKNOWN       LayerScope = 0
	LayerScope_REQUEST_SCOPE LayerScope = 1
	LayerScope_GLOBAL_SCOPE  LayerScope = 2
)

var (
	LayerScope_name = map[int32]string{
		0: "UNKNOWN",
		1: "REQUEST_SCOPE",
		2: "GLOBAL_SCOPE",
	}
	LayerScope_value = map[string]int32{
		"UNKNOWN":       0,
		"REQUEST_SCOPE": 1,
		"GLOBAL_SCOPE":  2,
	}
)

func (scope LayerScope) String() string {
	if name, ok := LayerScope_name[int32(scope)]; ok {
		return name
	}
	return LayerScope_name[0]
}

type GetConfigurationRequest struct{}

type GetConfigurationResponse struct {
	Controller *Controller `json:"controller,omitempty"`
	Render     *Render     `json:"render,omitempty"`
}

type Controller struct {
	Http []*HTTPComponent `json:"http,omitempty"`
}

type HTTPComponent struct {
	Id               string          `json:"id,omitempty"`
	Kind             string          `json:"kind,omitempty"`
	Handler          string          `json:"handler,omitempty"`
	HttpMethod       string          `json:"http_method,omitempty"`
	Route            string          `json:"route,omitempty"`
	ExceptionFilters []*Layer        `json:"exception_filters,omitempty"`
	Middlewares      []*Layer        `json:"middlewares,omitempty"`
	Guards           []*Layer        `json:"guards,omitempty"`
	Interceptors     []*Layer        `json:"interceptors,omitempty"`
	Versioning       *HTTPVersioning `json:"versioning,omitempty"`
	Request          *HTTPRequest    `json:"request,omitempty"`
}

type Render struct {
	IsReload      bool                  `json:"is_reload,omitempty"`
	Pages         []*RenderPage         `json:"pages,omitempty"`
	Partials      []string              `json:"partials,omitempty"`
	ErrorTemplate string                `json:"error_template,omitempty"`
	Assets        int                   `json:"assets,omitempty"`
	Renders       int64                 `json:"renders,omitempty"`
	Failures      int64                 `json:"failures,omitempty"`
	Counters      func() (int64, int64) `json:"-"`
}

func (r *Render) MarshalJSON() ([]byte, error) {
	type plain Render
	out := plain(*r)
	if r.Counters != nil {
		out.Renders, out.Failures = r.Counters()
	}
	return json.Marshal(out)
}

type RenderPage struct {
	Name   string `json:"name,omitempty"`
	Layout string `json:"layout,omitempty"`
}

type Layer struct {
	Scope LayerScope `json:"scope,omitempty"`
	Name  string     `json:"name,omitempty"`
}

type HTTPVersioning struct {
	Type  int32  `json:"type,omitempty"`
	Value string `json:"value,omitempty"`
	Key   string `json:"key,omitempty"`
}

type HTTPRequest struct {
	Body   []*Schema `json:"body,omitempty"`
	Form   []*Schema `json:"form,omitempty"`
	Query  []*Schema `json:"query,omitempty"`
	Header []*Schema `json:"header,omitempty"`
	Param  []*Schema `json:"param,omitempty"`
	File   []*Schema `json:"file,omitempty"`
}

type Schema struct {
	Name       string    `json:"name,omitempty"`
	Type       string    `json:"type,omitempty"`
	Format     string    `json:"format,omitempty"`
	Item       *Schema   `json:"item,omitempty"`
	Properties []*Schema `json:"properties,omitempty"`
}

type Devtool struct {
	GetConfigurationResponse
}

func (devtool *Devtool) GetConfiguration(context.Context, *GetConfigurationRequest) (*GetConfigurationResponse, error) {
	return &GetConfigurationResponse{
		Controller: devtool.Controller,
		Render:     devtool.Render,
	}, nil
}
