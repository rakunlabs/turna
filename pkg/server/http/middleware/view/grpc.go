package view

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/worldline-go/cache"

	"github.com/worldline-go/turna/pkg/server/http/httputil"
	"github.com/worldline-go/turna/pkg/server/http/middleware/grpcui"
	"github.com/worldline-go/turna/pkg/server/model"
)

type GrpcUI struct {
	grpcUIMiddlewares cache.Cacher[cacheKey, *grpcui.GrpcUI]

	m sync.Mutex
}

func (m *GrpcUI) Get(ctx context.Context, name, addr, prefixPath string) (*grpcui.GrpcUI, error) {
	m.m.Lock()
	defer m.m.Unlock()

	v, ok, err := m.grpcUIMiddlewares.Get(ctx, cacheKey{Name: name, Addr: addr})
	if err != nil {
		return nil, err
	}
	if ok {
		return v, nil
	}

	// set cache
	v = &grpcui.GrpcUI{
		BasePath: strings.TrimRight(prefixPath, "/") + "/grpc/" + name,
		Addr:     addr,
	}

	m.grpcUIMiddlewares.Set(ctx, cacheKey{Name: name, Addr: addr}, v)

	return v, nil
}

func findGrpcAddrList(nameList []string, grpcs []Grpc) (string, string) {
	var name string

	for i := range nameList {
		name += nameList[i]
		for _, g := range grpcs {
			if g.Name == name {
				return g.Addr, name
			}
		}
		name += "/"
	}

	return "", ""
}

func findGrpcAddr(name string, grpcs []Grpc) string {
	for _, g := range grpcs {
		if g.Name == name {
			return g.Addr
		}
	}

	return ""
}

func findGrpcAddrInGroup(oldName []string, name []string, groups []Group) (string, []string) {
	for _, g := range groups {
		if g.Name != getIndex(name, 0) {
			continue
		}

		lookGroup := append(oldName, g.Name)

		for _, s := range g.Services {
			if s.Name != getIndex(name, 1) {
				continue
			}

			serviceGroup := append(lookGroup, s.Name)

			if getIndex(name, 2) != "" {
				if addr := findGrpcAddr(name[2], s.Grpc); addr != "" {
					return addr, append(serviceGroup, name[2])
				}
			}
		}

		if addr, v := findGrpcAddrInGroup(lookGroup, name[1:], g.Groups); addr != "" {
			return addr, v
		}
	}

	return "", nil
}

func (m *View) GrpcUI(w http.ResponseWriter, r *http.Request, name string) {
	// get addr
	info, err := m.GetInfo(r.Context())
	if err != nil {
		httputil.JSON(w, http.StatusBadRequest, model.MetaData{Message: err.Error()})

		return
	}

	name = strings.Trim(name, "/")
	nameSplit := strings.Split(name, "/")

	addr, nameNew := findGrpcAddrList(nameSplit, info.Grpc)
	if addr != "" {
		name = nameNew
	} else {
		var nameL []string
		addr, nameL = findGrpcAddrInGroup(nil, nameSplit, info.Groups)
		name = strings.Join(nameL, "/")
	}

	if addr == "" {
		httputil.JSON(w, http.StatusBadRequest, model.MetaData{Message: name + " not found any addr"})

		return
	}

	gUI, err := m.grpcUI.Get(r.Context(), name, addr, m.PrefixPath)
	if err != nil {
		httputil.JSON(w, http.StatusBadRequest, model.MetaData{Message: err.Error()})
		return
	}

	gUI.Middleware()(nil).ServeHTTP(w, r)
}
