package http

import (
	"bufio"
	"net"
	stdhttp "net/http"

	"github.com/palma99/palma-framework/logging"
)

// HandlerAdapter maps returned errors and logs failures at the net/http boundary.
// Wrap returns an ordinary HandlerFunc, composable with native middleware.
type HandlerAdapter struct {
	mapper ErrorMapper
	logger logging.Logger
}

func NewHandlerAdapter(mapper ErrorMapper, logger logging.Logger) *HandlerAdapter {
	if mapper == nil {
		mapper = NewMapper()
	}
	return &HandlerAdapter{mapper: mapper, logger: logger}
}

func (a *HandlerAdapter) Wrap(handler func(stdhttp.ResponseWriter, *stdhttp.Request) error) stdhttp.HandlerFunc {
	return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		response := &trackedResponse{ResponseWriter: w}
		if err := handler(response, r); err != nil {
			log := func(message string, err error) {
				if a.logger != nil {
					a.logger.Error(r.Context(), message, "error", err)
				}
			}
			if response.committed {
				log("HTTP response failed", err)
				return
			}
			mapped := a.mapper.Map(err)
			if mapped == nil {
				return
			}
			if mapped.Status >= 500 {
				log("request failed", err)
			}
			if err := mapped.Write(response); err != nil {
				log("HTTP response failed", err)
			}
		}
	}
}

type trackedResponse struct {
	stdhttp.ResponseWriter
	committed bool
}

// Unwrap supports http.ResponseController operations on the original writer.
func (w *trackedResponse) Unwrap() stdhttp.ResponseWriter { return w.ResponseWriter }

func (w *trackedResponse) WriteHeader(status int) {
	if status >= 200 || status == stdhttp.StatusSwitchingProtocols {
		w.committed = true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *trackedResponse) Write(data []byte) (int, error) {
	w.committed = true
	return w.ResponseWriter.Write(data)
}

func (w *trackedResponse) FlushError() error {
	w.committed = true
	return stdhttp.NewResponseController(w.ResponseWriter).Flush()
}

func (w *trackedResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buffer, err := stdhttp.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.committed = true
	}
	return conn, buffer, err
}
