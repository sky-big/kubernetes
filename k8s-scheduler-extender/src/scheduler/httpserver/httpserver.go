package httpserver

import (
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"scheduler/configure"
	"scheduler/handler"
	"util"

	"github.com/golang/glog"
	"github.com/julienschmidt/httprouter"
)

type HttpServer struct {
	router       http.Handler
	httpListener net.Listener
	waitGroup    util.WaitGroupWrapper
	handler      *handler.Handler
}

type Decorator func(APIHandler) APIHandler

type APIHandler func(http.ResponseWriter, *http.Request, httprouter.Params) error

func NewHttpServer() (*HttpServer, error) {
	// new handler
	h, err := handler.NewHandler()
	if err != nil {
		return nil, err
	}
	// run handler
	if err := h.Run(); err != nil {
		return nil, err
	}

	// init http router
	router := httprouter.New()
	// init http server
	s := &HttpServer{
		router:  router,
		handler: h,
	}
	router.HandleMethodNotAllowed = true
	httpLogDecorator := s.httpRequestLog()

	// handler
	router.Handle("POST", "/storage/scheduler/predicate", decorate(h.StorageSchedulerPredicate, httpLogDecorator))
	router.Handle("POST", "/storage/scheduler/priority", decorate(h.StorageSchedulerPriority, httpLogDecorator))

	return s, nil
}

func (s *HttpServer) Run() error {
	httpListener, err := net.Listen("tcp", configure.Conf.HttpListen)
	if err != nil {
		glog.Fatalf("listen (%s) failed - %s", configure.Conf.HttpListen, err)
		os.Exit(1)
	}

	s.httpListener = httpListener
	s.waitGroup.Wrap(func() {
		s.serve(httpListener, "HTTP")
	})

	return nil
}

func (s *HttpServer) serve(listener net.Listener, proto string) {
	glog.Infof("%s: listening on %s", proto, listener.Addr())
	server := &http.Server{
		Handler: s,
	}

	err := server.Serve(listener)
	// there is no direct way to detect this error because it is not exposed
	if err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
		glog.Errorf("ERROR: http.Serve() - %s", err)
	}

	glog.Infof("%s: closing %s", proto, listener.Addr())
}

func (s *HttpServer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	s.router.ServeHTTP(w, req)
}

func (s *HttpServer) httpRequestLog() Decorator {
	return func(f APIHandler) APIHandler {
		return func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) error {
			start := time.Now()
			err := f(w, req, ps)
			elapsed := time.Since(start)
			status := 200
			glog.Infof("%d %s %s (%s) %s", status, req.Method, req.URL.RequestURI(), req.RemoteAddr, elapsed)
			return err
		}
	}
}

func decorate(f APIHandler, ds ...Decorator) httprouter.Handle {
	decorated := f
	for _, decorate := range ds {
		decorated = decorate(decorated)
	}

	return func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		decorated(w, req, ps)
	}
}

func (s *HttpServer) Stop() {
	s.httpListener.Close()
	s.waitGroup.Wait()
}
