// +build !linux

package httpd

func (s *Server) ListenAndServeTLS(certFile, keyFile string) error {
	s.prepareToServe()
	ln, err := s.bindListener("https")
	if err == nil {
		defer ln.Close()
		s.justBeforeServing(ln, "https", "")
		err = s.Server.ServeTLS(ln, certFile, keyFile)
	}
	return s.returnFromServe(err)
}

func (s *Server) ListenAndServe() error {
	s.prepareToServe()
	ln, err := s.bindListener("http")
	if err == nil {
		defer ln.Close()
		s.justBeforeServing(ln, "http", "")
		err = s.Server.Serve(ln)
	}
	return s.returnFromServe(err)
}
