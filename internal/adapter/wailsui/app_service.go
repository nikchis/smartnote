package wailsui

// AppService lets the interface finish the close scenario.
type AppService struct {
	lifecycle *Lifecycle
}

// NewAppService returns an AppService over l.
func NewAppService(l *Lifecycle) *AppService { return &AppService{lifecycle: l} }

// ConfirmQuit quits the application. It returns at once: quitting closes the
// window that waits for the answer to this call.
func (s *AppService) ConfirmQuit() { go s.lifecycle.ConfirmQuit() }
