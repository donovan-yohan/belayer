package daemon

// addForTest injects a blocked goroutine into the archiveManager's WaitGroup
// without ever calling Done, so DrainAll-timeout tests can simulate a stuck
// archive. The returned release func must be called to unblock the goroutine,
// keeping the test leak-free on cleanup. Test-only API; lives in _test.go so
// it stays out of the production binary.
func (m *archiveManager) addForTest() (release func()) {
	ch := make(chan struct{})
	m.inflight.Add(1)
	go func() {
		<-ch
		m.inflight.Done()
	}()
	return func() { close(ch) }
}
