// +build linux

package httpd

import (
	// "context"
	// "fmt"
	// "net"
	// "net/http"
	// "os"
	// "os/signal"
	// "sync"
	// "syscall"
	// "time"

	"github.com/coreos/go-systemd/activation"
)

// This file implements ListenAndServer which works with systemd socket activation.
// Inspired by https://vincent.bernat.ch/en/blog/2018-systemd-golang-socket-activation

func (s *Server) ListenAndServeTLS(certFile, keyFile string) error {
	// TODO
	s.prepareToServe()
	err := s.Server.ListenAndServeTLS(certFile, keyFile)
	return s.returnFromServe(err)
}

func (s *Server) ListenAndServe() error {
	s.prepareToServe()

	// get listeners from systemd
	listeners, err := activation.Listeners()
	if err != nil {
		// Note: current implementation of go-systemd/activation never returns an error so it's
		// unclear under what conditions it might do so in the future.
		return err
	}

	if len(listeners) == 0 {
		// no systemd socket for this process
		err = s.Server.ListenAndServe()
	} else if len(listeners) != 1 {
		// We can only handle a single socket; fail if we get more than 1.
		// If multiple sockets are provided by systemd for the process, it's better to call Serve(l)
		// directly instead of using ListenSystemd()
		panic(errorf("More than one socket fds from systemd: %d", len(listeners)))
	} else {
		// start accepting connections from the systemd-provided socket
		s.logd("using socket from systemd socket activation")
		l := listeners[0]
		err = s.Serve(l)
	}

	return s.returnFromServe(err)
}
