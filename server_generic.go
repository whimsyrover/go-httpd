// +build !linux

package httpd

func (s *Server) ListenAndServeTLS(certFile, keyFile string) error {
	s.prepareToServe()
	err := s.Server.ListenAndServeTLS(certFile, keyFile)
	return s.returnFromServe(err)
}

func (s *Server) ListenAndServe() error {
	s.prepareToServe()
	err := s.Server.ListenAndServe()
	return s.returnFromServe(err)
}
