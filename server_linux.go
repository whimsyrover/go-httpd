// +build linux

package httpd

import (
	"net"

	"github.com/coreos/go-systemd/activation"
)

// This file implements ListenAndServer which works with systemd socket activation.
// Inspired by https://vincent.bernat.ch/en/blog/2018-systemd-golang-socket-activation

func (s *Server) ListenAndServeTLS(certFile, keyFile string) error {
	ln, err := s.listenSystemd("https")
	if err == nil {
		defer ln.Close()
		err = s.Server.ServeTLS(ln, certFile, keyFile)
	}
	return s.returnFromServe(err)
}

func (s *Server) ListenAndServe() error {
	ln, err := s.listenSystemd("http")
	if err == nil {
		defer ln.Close()
		err = s.Server.Serve(ln)
	}
	return s.returnFromServe(err)
}

func (s *Server) listenSystemd(proto string) (net.Listener, error) {
	var ln net.Listener
	s.prepareToServe()
	listeners, err := activation.Listeners()
	if err == nil && len(listeners) == 1 {
		// use systemd listener
		s.Logger.Debug("using socket from systemd socket activation")
		ln = listeners[0]
		s.justBeforeServing(ln, proto, ", systemd-socket")
	} else {
		if len(listeners) > 1 {
			// We can only handle a single socket; fail if we get more than 1.
			// If multiple sockets are provided by systemd for the process, it's better to call Serve(l)
			// directly instead of using ListenSystemd()
			s.Logger.Warn("More than one socket fds from systemd: %d (ignoring systemd socket)",
				len(listeners))
		}
		ln, err = s.bindListener(proto)
		if err == nil {
			s.justBeforeServing(ln, proto, "")
		}
	}
	return ln, err
}
