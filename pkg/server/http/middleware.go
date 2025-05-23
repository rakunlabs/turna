package http

import (
	"context"
	"net/http"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/accesslog"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/addprefix"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/basicauth"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/block"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/cors"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/decompress"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/dnspath"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/folder"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/forward"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/grpcui"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/gzip"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/headers"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/hello"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iamcheck"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/info"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/inject"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/log"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/login"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/path"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/print"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/redirectcontinue"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/redirection"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/regexpath"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/request"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/requestid"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/role"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/rolecheck"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/roledata"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/scope"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/service"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/sessioninfo"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/set"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/splitter"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/stripprefix"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/template"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/tokenpass"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/try"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/view"

	"github.com/rakunlabs/turna/pkg/server/registry"
)

type MiddlewareFunc = func(http.Handler) http.Handler

type HTTPMiddleware struct {
	AddPrefixMiddleware        *addprefix.AddPrefix                  `cfg:"add_prefix"`
	InjectMiddleware           *inject.Inject                        `cfg:"inject"`
	HelloMiddleware            *hello.Hello                          `cfg:"hello"`
	TemplateMiddleware         *template.Template                    `cfg:"template"`
	InfoMiddleware             *info.Info                            `cfg:"info"`
	SetMiddleware              *set.Set                              `cfg:"set"`
	StripPrefixMiddleware      *stripprefix.StripPrefix              `cfg:"strip_prefix"`
	RoleMiddleware             *role.Role                            `cfg:"role"`
	ScopeMiddleware            *scope.Scope                          `cfg:"scope"`
	ServiceMiddleware          *service.Service                      `cfg:"service"`
	FolderMiddleware           *folder.Folder                        `cfg:"folder"`
	BasicAuthMiddleware        *basicauth.BasicAuth                  `cfg:"basic_auth"`
	CorsMiddleware             *cors.Cors                            `cfg:"cors"`
	HeadersMiddleware          *headers.Headers                      `cfg:"headers"`
	BlockMiddleware            *block.Block                          `cfg:"block"`
	RegexPathMiddleware        *regexpath.RegexPath                  `cfg:"regex_path"`
	GzipMiddleware             *gzip.Gzip                            `cfg:"gzip"`
	DecompressMiddleware       *decompress.Decompress                `cfg:"decompress"`
	LogMiddleware              *log.Log                              `cfg:"log"`
	PrintMiddleware            *print.Print                          `cfg:"print"`
	LoginMiddleware            *login.Login                          `cfg:"login"`
	SessionMiddleware          *session.Session                      `cfg:"session"`
	ViewMiddleware             *view.View                            `cfg:"view"`
	RequestMiddleware          *request.Request                      `cfg:"request"`
	RedirectionMiddleware      *redirection.Redirection              `cfg:"redirection"`
	TryMiddleware              *try.Try                              `cfg:"try"`
	SessionInfoMiddleware      *sessioninfo.Info                     `cfg:"session_info"`
	IamMiddleware              *iam.Iam                              `cfg:"iam"`
	IamCheckMiddleware         *iamcheck.IamCheck                    `cfg:"iam_check"`
	RoleCheckMiddleware        *rolecheck.RoleCheck                  `cfg:"role_check"`
	RoleDataMiddleware         *roledata.RoleData                    `cfg:"role_data"`
	TokenPassMiddleware        *tokenpass.TokenPass                  `cfg:"token_pass"`
	RedirectContinueMiddleware *redirectcontinue.RedirectionContinue `cfg:"redirect_continue"`
	ForwardMiddleware          *forward.Forward                      `cfg:"forward"`
	GrpcUIMiddleware           *grpcui.GrpcUI                        `cfg:"grpcui"`
	DNSPathMiddleware          *dnspath.DNSPath                      `cfg:"dns_path"`
	SplitterMiddleware         *splitter.Splitter                    `cfg:"splitter"`
	PathMiddleware             *path.Path                            `cfg:"path"`
	RequestIDMiddleware        *requestid.RequestID                  `cfg:"request_id"`
	Oauth2                     *oauth2.Oauth2                        `cfg:"oauth2"`
	AccessLogMiddleware        *accesslog.AccessLog                  `cfg:"access_log"`
}

func (h *HTTPMiddleware) getFirstFound(ctx context.Context, name string) ([]MiddlewareFunc, error) {
	switch {
	case h.AddPrefixMiddleware != nil:
		return []MiddlewareFunc{h.AddPrefixMiddleware.Middleware()}, nil
	case h.InjectMiddleware != nil:
		m, err := h.InjectMiddleware.Middleware()
		return []MiddlewareFunc{m}, err
	case h.HelloMiddleware != nil:
		m, err := h.HelloMiddleware.Middleware()
		return []MiddlewareFunc{m}, err
	case h.TemplateMiddleware != nil:
		m, err := h.TemplateMiddleware.Middleware()
		return []MiddlewareFunc{m}, err
	case h.InfoMiddleware != nil:
		registry.GlobalReg.AddInitFunc(name, h.InfoMiddleware.Init)
		return []MiddlewareFunc{h.InfoMiddleware.Middleware()}, nil
	case h.SetMiddleware != nil:
		return []MiddlewareFunc{h.SetMiddleware.Middleware()}, nil
	case h.StripPrefixMiddleware != nil:
		return []MiddlewareFunc{h.StripPrefixMiddleware.Middleware()}, nil
	case h.RoleMiddleware != nil:
		return []MiddlewareFunc{h.RoleMiddleware.Middleware()}, nil
	case h.ScopeMiddleware != nil:
		return []MiddlewareFunc{h.ScopeMiddleware.Middleware()}, nil
	case h.ServiceMiddleware != nil:
		m, err := h.ServiceMiddleware.Middleware()
		return m, err
	case h.FolderMiddleware != nil:
		m, err := h.FolderMiddleware.Middleware()
		return []MiddlewareFunc{m}, err
	case h.BasicAuthMiddleware != nil:
		m, err := h.BasicAuthMiddleware.Middleware(name)
		return []MiddlewareFunc{m}, err
	case h.CorsMiddleware != nil:
		return []MiddlewareFunc{h.CorsMiddleware.Middleware()}, nil
	case h.HeadersMiddleware != nil:
		return []MiddlewareFunc{h.HeadersMiddleware.Middleware()}, nil
	case h.BlockMiddleware != nil:
		m, err := h.BlockMiddleware.Middleware()
		return []MiddlewareFunc{m}, err
	case h.RegexPathMiddleware != nil:
		m, err := h.RegexPathMiddleware.Middleware()
		return []MiddlewareFunc{m}, err
	case h.GzipMiddleware != nil:
		return []MiddlewareFunc{h.GzipMiddleware.Middleware()}, nil
	case h.DecompressMiddleware != nil:
		return []MiddlewareFunc{h.DecompressMiddleware.Middleware()}, nil
	case h.LogMiddleware != nil:
		m, err := h.LogMiddleware.Middleware()
		return []MiddlewareFunc{m}, err
	case h.PrintMiddleware != nil:
		m, err := h.PrintMiddleware.Middleware()
		return []MiddlewareFunc{m}, err
	case h.LoginMiddleware != nil:
		registry.GlobalReg.AddInitFunc(name, h.LoginMiddleware.Init)
		m, err := h.LoginMiddleware.Middleware(ctx)
		return []MiddlewareFunc{m}, err
	case h.SessionMiddleware != nil:
		m, err := h.SessionMiddleware.Middleware(ctx, name)
		return []MiddlewareFunc{m}, err
	case h.ViewMiddleware != nil:
		m, err := h.ViewMiddleware.Middleware(ctx, name)
		return []MiddlewareFunc{m}, err
	case h.RequestMiddleware != nil:
		m, err := h.RequestMiddleware.Middleware()
		return []MiddlewareFunc{m}, err
	case h.RedirectionMiddleware != nil:
		return []MiddlewareFunc{h.RedirectionMiddleware.Middleware()}, nil
	case h.TryMiddleware != nil:
		m, err := h.TryMiddleware.Middleware()
		return []MiddlewareFunc{m}, err
	case h.SessionInfoMiddleware != nil:
		m, err := h.SessionInfoMiddleware.Middleware()
		return []MiddlewareFunc{m}, err
	case h.IamMiddleware != nil:
		m, err := h.IamMiddleware.Middleware(ctx, name)
		return []MiddlewareFunc{m}, err
	case h.IamCheckMiddleware != nil:
		m, err := h.IamCheckMiddleware.Middleware()
		return []MiddlewareFunc{m}, err
	case h.RoleCheckMiddleware != nil:
		m, err := h.RoleCheckMiddleware.Middleware()
		return []MiddlewareFunc{m}, err
	case h.RoleDataMiddleware != nil:
		m, err := h.RoleDataMiddleware.Middleware()
		return []MiddlewareFunc{m}, err
	case h.TokenPassMiddleware != nil:
		m, err := h.TokenPassMiddleware.Middleware()
		return []MiddlewareFunc{m}, err
	case h.RedirectContinueMiddleware != nil:
		m, err := h.RedirectContinueMiddleware.Middleware()
		return []MiddlewareFunc{m}, err
	case h.ForwardMiddleware != nil:
		m, err := h.ForwardMiddleware.Middleware()
		return []MiddlewareFunc{m}, err
	case h.GrpcUIMiddleware != nil:
		return []MiddlewareFunc{h.GrpcUIMiddleware.Middleware()}, nil
	case h.DNSPathMiddleware != nil:
		m, err := h.DNSPathMiddleware.Middleware()
		return []MiddlewareFunc{m}, err
	case h.SplitterMiddleware != nil:
		m, err := h.SplitterMiddleware.Middleware()
		return []MiddlewareFunc{m}, err
	case h.PathMiddleware != nil:
		m := h.PathMiddleware.Middleware()
		return []MiddlewareFunc{m}, nil
	case h.RequestIDMiddleware != nil:
		m := h.RequestIDMiddleware.Middleware()
		return []MiddlewareFunc{m}, nil
	case h.Oauth2 != nil:
		registry.GlobalReg.AddInitFunc(name, h.Oauth2.Init)
		m, err := h.Oauth2.Middleware(ctx)
		return []MiddlewareFunc{m}, err
	case h.AccessLogMiddleware != nil:
		m, err := h.AccessLogMiddleware.Middleware()
		return []MiddlewareFunc{m}, err
	}

	return nil, nil
}

func (h *HTTPMiddleware) Set(ctx context.Context, name string) error {
	middleware, err := h.getFirstFound(ctx, name)
	if err != nil {
		return err
	}

	registry.GlobalReg.AddHttpMiddleware(name, middleware)

	return nil
}
