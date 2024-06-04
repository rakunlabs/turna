package view

import (
	"net/http"
	"strings"
	"sync"

	"github.com/labstack/echo/v4"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/grpcui"
	"github.com/rakunlabs/turna/pkg/server/model"
)

type GrpcUI struct {
	grpcUIMiddlewares []GrpcUIMWrap

	m sync.Mutex
}

type GrpcUIMWrap struct {
	Name string
	Addr string

	GrpcUI *grpcui.GrpcUI
}

func (m *GrpcUI) Get(name, addr string) *grpcui.GrpcUI {
	m.m.Lock()
	defer m.m.Unlock()

	for _, g := range m.grpcUIMiddlewares {
		if g.Name == name && g.Addr == addr {
			return g.GrpcUI
		}
	}

	return nil
}

func (m *GrpcUI) Set(name, addr, prefixPath string) *grpcui.GrpcUI {
	m.m.Lock()
	defer m.m.Unlock()

	v := GrpcUIMWrap{
		Name: name,
		Addr: addr,
		GrpcUI: &grpcui.GrpcUI{
			BasePath: strings.TrimRight(prefixPath, "/") + "/grpc/" + name,
			Addr:     addr,
		},
	}

	m.grpcUIMiddlewares = append(m.grpcUIMiddlewares, v)

	return v.GrpcUI
}

func (m *View) GrpcUI(c echo.Context, name string) error {
	// get addr
	info, err := m.GetInfo(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusBadRequest, model.MetaData{Message: err.Error()})
	}

	addr := ""
	for _, v := range info.Grpc {
		if v.Name == name {
			addr = v.Addr

			break
		}
	}

	if addr == "" {
		return c.JSON(http.StatusBadRequest, model.MetaData{Message: name + " not found any addr"})
	}

	gUI := m.grpcUI.Get(name, addr)
	if gUI == nil {
		gUI = m.grpcUI.Set(name, addr, m.PrefixPath)
	}

	gUI.Middleware()(nil).ServeHTTP(c.Response(), c.Request())

	return nil
}
