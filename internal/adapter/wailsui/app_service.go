package wailsui

// AppService lets the interface take part in quitting: it reports that it
// listens for the close request and finishes the close scenario.
type AppService struct {
	lifecycle *Lifecycle
}

// NewAppService returns an AppService over l.
func NewAppService(l *Lifecycle) *AppService { return &AppService{lifecycle: l} }

// ConfirmQuit quits the application. It returns at once: quitting closes the
// window that waits for the answer to this call.
func (s *AppService) ConfirmQuit() { go s.lifecycle.ConfirmQuit() }

// Ready tells that the interface listens for the close request; until then
// closing the window quits without asking.
func (s *AppService) Ready() { s.lifecycle.Ready() }
