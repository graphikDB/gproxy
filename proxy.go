package gproxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/autom8ter/machine"
	"github.com/graphikDB/gproxy/codec"
	"github.com/graphikDB/gproxy/logger"
	"github.com/graphikDB/trigger"
	"github.com/mwitkow/grpc-proxy/proxy"
	"github.com/pkg/errors"
	"github.com/soheilhy/cmux"
	"go.uber.org/zap"
	"golang.org/x/crypto/acme/autocert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

func init() {
	encoding.RegisterCodec(codec.NewProxyCodec())
}

// Proxy is a secure(lets encrypt) gRPC & http reverse proxy
type Proxy struct {
	mu            sync.RWMutex
	mach          *machine.Machine
	logger        *logger.Logger
	triggers      []*trigger.Trigger
	hostPolicy    autocert.HostPolicy
	certCache     string
	insecurePort  string
	securePort    string
	redirectHttps bool
	httpInit      []func(srv *http.Server)
	httpsInit     []func(srv *http.Server)
	grpcInit      []func(srv *grpc.Server)
	grpcsInit     []func(srv *grpc.Server)
	grpcOpts      []grpc.ServerOption
	grpcsOpts     []grpc.ServerOption
}

// New creates a new proxy instance. A host policy & either http routes, gRPC routes, or both are required.
func New(ctx context.Context, opts ...Opt) (*Proxy, error) {
	p := &Proxy{}
	for _, o := range opts {
		if err := o(p); err != nil {
			return nil, err
		}
	}
	if len(p.triggers) == 0 {
		return nil, errors.New("zero triggers")
	}
	if p.hostPolicy == nil {
		return nil, errors.New("empty host policy")
	}
	if p.insecurePort == "" {
		p.insecurePort = ":80"
	}
	if p.securePort == "" {
		p.securePort = ":443"
	}
	if p.logger == nil {
		p.logger = logger.New(false)
	}
	if p.mach == nil {
		p.mach = machine.New(ctx)
	}

	if p.certCache == "" {
		p.certCache = "/tmp/certs"
	}
	p.mu = sync.RWMutex{}
	os.MkdirAll(p.certCache, 0700)
	return p, nil
}

// Serve starts the gRPC(if grpc router was registered) & http proxy(if http router was registered)
func (p *Proxy) Serve(ctx context.Context) error {
	var (
		m = &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: p.hostPolicy,
			Cache:      autocert.DirCache(p.certCache),
		}
		tlsConfig = &tls.Config{
			GetCertificate: m.GetCertificate,
		}
		shutdown []func(ctx context.Context)
	)
	insecure, err := net.Listen("tcp", p.insecurePort)
	if err != nil {
		return err
	}
	secure, err := tls.Listen("tcp", p.securePort, tlsConfig)
	if err != nil {
		return err
	}
	defer insecure.Close()
	defer secure.Close()
	imux := cmux.New(insecure)
	smux := cmux.New(secure)
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupt)
	var httpHandler http.Handler
	if p.redirectHttps {
		httpHandler = m.HTTPHandler(nil)
	} else {
		httpHandler = m.HTTPHandler(&httputil.ReverseProxy{
			Director: p.httpDirector(),
		})
	}
	httpServer := &http.Server{
		Handler: httpHandler,
	}
	for _, o := range p.httpInit {
		o(httpServer)
	}
	p.mach.Go(func(routine machine.Routine) {
		matcher := imux.Match(cmux.Any())
		p.logger.Debug("starting http server", zap.String("address", matcher.Addr().String()))
		if err := httpServer.Serve(matcher); err != nil && err != http.ErrServerClosed &&
			!strings.Contains(err.Error(), "mux: listener closed") {
			p.logger.Error("http proxy failure", zap.Error(err))
		}
	})
	shutdown = append(shutdown, func(ctx context.Context) {
		_ = httpServer.Shutdown(ctx)
	})
	var httpsHandler = m.HTTPHandler(&httputil.ReverseProxy{
		Director: p.httpDirector(),
	})
	tlsHttpServer := &http.Server{
		Handler: httpsHandler,
	}

	for _, o := range p.httpsInit {
		o(tlsHttpServer)
	}
	p.mach.Go(func(routine machine.Routine) {
		matcher := smux.Match(cmux.Any())
		p.logger.Debug("starting secure http server", zap.String("address", matcher.Addr().String()))
		if err := tlsHttpServer.Serve(matcher); err != nil &&
			err != http.ErrServerClosed &&
			!strings.Contains(err.Error(), "mux: listener closed") {
			p.logger.Error("TLS http proxy failure", zap.Error(err))
		}
	})

	shutdown = append(shutdown, func(ctx context.Context) {
		_ = tlsHttpServer.Shutdown(ctx)
	})
	gopts := []grpc.ServerOption{
		grpc.UnknownServiceHandler(proxy.TransparentHandler(p.gRPCDirector())),
	}
	for _, o := range p.grpcOpts {
		gopts = append(gopts, o)
	}
	gserver := grpc.NewServer(gopts...)
	for _, o := range p.grpcInit {
		o(gserver)
	}
	p.mach.Go(func(routine machine.Routine) {
		matcher := imux.MatchWithWriters(cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"))
		p.logger.Debug("starting gRPC server", zap.String("address", matcher.Addr().String()))
		if err := gserver.Serve(matcher); err != nil && !strings.Contains(err.Error(), "mux: listener closed") {
			p.logger.Error("gRPC proxy failure", zap.Error(err))
		}
	})
	shutdown = append(shutdown, func(ctx context.Context) {
		stopped := make(chan struct{}, 1)
		go func() {
			gserver.GracefulStop()
			stopped <- struct{}{}
		}()
		select {
		case <-ctx.Done():
			gserver.Stop()
		case <-stopped:
			return
		}
	})
	gsopts := []grpc.ServerOption{
		grpc.UnknownServiceHandler(proxy.TransparentHandler(p.gRPCDirector())),
	}
	for _, o := range p.grpcsOpts {
		gsopts = append(gsopts, o)
	}
	tlsGserver := grpc.NewServer(gsopts...)
	for _, o := range p.grpcsInit {
		o(tlsGserver)
	}
	p.mach.Go(func(routine machine.Routine) {
		matcher := smux.MatchWithWriters(cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"))
		p.logger.Debug("starting secure gRPC server", zap.String("address", matcher.Addr().String()))
		if err := tlsGserver.Serve(matcher); err != nil && !strings.Contains(err.Error(), "mux: listener closed") {
			p.logger.Error("TLS gRPC proxy failure", zap.Error(err))
		}
	})
	shutdown = append(shutdown, func(ctx context.Context) {
		stopped := make(chan struct{}, 1)
		go func() {
			tlsGserver.GracefulStop()
			stopped <- struct{}{}
		}()
		select {
		case <-ctx.Done():
			tlsGserver.Stop()
		case <-stopped:
			return
		}
	})

	p.mach.Go(func(routine machine.Routine) {
		if err := imux.Serve(); err != nil && !strings.Contains(err.Error(), "closed network connection") {
			p.logger.Error("listener mux error", zap.Error(err))
		}
	})
	p.mach.Go(func(routine machine.Routine) {
		if err := smux.Serve(); err != nil && !strings.Contains(err.Error(), "closed network connection") {
			p.logger.Error("listener mux error", zap.Error(err))
		}
	})
	select {
	case <-interrupt:
		p.mach.Close()
		break
	case <-ctx.Done():
		p.mach.Close()
		break
	}
	p.logger.Debug("shutdown signal received")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	wg := &sync.WaitGroup{}
	for _, closer := range shutdown {
		wg.Add(1)
		go func(c func(ctx context.Context)) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(shutdownCtx, 5*time.Second)
			defer cancel()
			c(ctx)
		}(closer)
	}
	wg.Wait()
	p.mach.Wait()
	p.logger.Debug("shutdown successful")
	return nil
}

// OverrideTriggers overrides the triggers-routes on the Proxy. It is concurrency safe
func (p *Proxy) OverrideTriggers(expressions []string) error {
	var triggers []*trigger.Trigger
	for _, exp := range expressions {
		t, err := trigger.NewArrowTrigger(exp)
		if err != nil {
			return err
		}
		triggers = append(triggers, t)
	}
	p.mu.Lock()
	p.triggers = triggers
	p.mu.Unlock()
	return nil
}

func (p *Proxy) gRPCDirector() proxy.StreamDirector {
	return func(ctx context.Context, fullMethodName string) (context.Context, *grpc.ClientConn, error) {
		ctx = invertContext(ctx)
		md, ok := metadata.FromIncomingContext(ctx)
		if ok {
			if val, exists := md[":authority"]; exists && val[0] != "" {
				now := time.Now()
				target, err := p.getgRPCRoute(val[0], fullMethodName, md)
				if err != nil {
					return nil, nil, status.Error(codes.InvalidArgument, err.Error())
				}
				fields := []zap.Field{
					zap.String("proxy", "gRPC"),
					zap.Any("metadata", md),
					zap.String("method", fullMethodName),
					zap.String("target", target),
				}
				defer func() {
					dur := time.Since(now)
					fields = append(fields, zap.Duration("duration", dur))
					p.logger.Debug("proxied request", fields...)
				}()
				if target == "" {
					return nil, nil, status.Error(codes.PermissionDenied, "unknown route")
				}

				// Make sure we use DialContext so the dialing can be cancelled/time out together with the context.
				conn, err := grpc.DialContext(invertContext(ctx), target, grpc.WithInsecure())
				if err != nil {
					return nil, nil, err
				}
				return ctx, conn, err
			}
		}
		return nil, nil, status.Error(codes.Unimplemented, "Unknown method")
	}
}

func (p *Proxy) httpDirector() func(r *http.Request) {
	return func(req *http.Request) {
		now := time.Now()
		fields := []zap.Field{
			zap.String("proxy", "http"),
			zap.String("host", req.Host),
			zap.String("method", req.Method),
			zap.Any("headers", req.Header),
		}
		defer func() {
			dur := time.Since(now)
			fields = append(fields, zap.Duration("duration", dur))
			p.logger.Debug("proxied request", fields...)
		}()

		target, err := p.getHttpRoute(req)
		if err != nil {
			p.logger.Error("failed to find routing target", zap.Error(err))
			return
		}
		fields = append(fields, zap.String("target", target))
		if target == "" {
			p.logger.Debug("empty routing target", fields...)
			return
		} else {
			u, err := url.Parse(target)
			if err != nil {
				fields = append(fields, zap.Error(err))
				p.logger.Debug("failed to parse target", fields...)
				return
			}
			req.URL.Scheme = u.Scheme
			req.URL.Host = u.Host
			req.URL.Path, req.URL.RawPath = joinURLPath(u, req.URL)
			if u.RawQuery == "" || req.URL.RawQuery == "" {
				req.URL.RawQuery = u.RawQuery + req.URL.RawQuery
			} else {
				req.URL.RawQuery = u.RawQuery + "&" + req.URL.RawQuery
			}
			if _, ok := req.Header["User-Agent"]; !ok {
				// explicitly disable User-Agent so it's not set to default value
				req.Header.Set("User-Agent", "")
			}
		}
	}
}

// (this.http, this.grpc, this.host, this.headers, this.path)
func (p *Proxy) getHttpRoute(req *http.Request) (string, error) {
	headers := map[string]interface{}{}
	for k, v := range req.Header {
		headers[k] = v[0]
	}
	data := map[string]interface{}{
		"http":    true,
		"grpc":    false,
		"host":    req.Host,
		"headers": headers,
		"path":    req.URL.Path,
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, trig := range p.triggers {
		result, err := trig.Trigger(data)
		if err == nil {
			target, ok := result["value"].(string)
			if ok {
				if !strings.Contains(target, "http") {
					target = fmt.Sprintf("http://%s", target)
				}
				return target, nil
			}
		}
	}
	return "", errors.New("zero http routes for request")
}

// (this.http, this.grpc, this.host, this.headers, this.path)
func (p *Proxy) getgRPCRoute(host, fullMethod string, md metadata.MD) (string, error) {
	meta := map[string]interface{}{}
	for k, v := range md {
		meta[k] = v[0]
	}
	data := map[string]interface{}{
		"http":    false,
		"grpc":    true,
		"host":    host,
		"path":    fullMethod,
		"headers": meta,
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, trig := range p.triggers {
		result, err := trig.Trigger(data)
		if err == nil {
			target, ok := result["value"].(string)
			if ok {
				return target, nil
			}
		}
	}
	return "", errors.New("zero gRPC routes for request")
}

func joinURLPath(a, b *url.URL) (path, rawpath string) {
	if a.RawPath == "" && b.RawPath == "" {
		return singleJoiningSlash(a.Path, b.Path), ""
	}
	// Same as singleJoiningSlash, but uses EscapedPath to determine
	// whether a slash should be added
	apath := a.EscapedPath()
	bpath := b.EscapedPath()

	aslash := strings.HasSuffix(apath, "/")
	bslash := strings.HasPrefix(bpath, "/")

	switch {
	case aslash && bslash:
		return a.Path + b.Path[1:], apath + bpath[1:]
	case !aslash && !bslash:
		return a.Path + "/" + b.Path, apath + "/" + bpath
	}
	return a.Path + b.Path, apath + bpath
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}

func invertContext(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		return metadata.NewOutgoingContext(ctx, md)
	}

	return ctx
}
