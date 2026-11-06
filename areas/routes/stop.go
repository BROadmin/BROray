package routes

// routeStopState belongs to one operation in this process. A stale browser
// request cannot stop a later job on the same bundle.
type routeStopState struct {
	operationID string
	accepting   bool
	requested   bool
}

func (e *engine) enableRouteStop(bundle, operationID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if s := e.stops[bundle]; s != nil && s.operationID == operationID && operationID != "" {
		s.accepting = true
	}
}

func (e *engine) requestRouteStop(bundle, operationID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.stops[bundle]
	if s == nil || !e.active[bundle] || !s.accepting || s.operationID != operationID || operationID == "" {
		return false
	}
	s.requested = true
	return true
}

func (e *engine) routeStopSnapshot(bundle, operationID string) (canStop, requested bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.stops[bundle]
	if s == nil || !e.active[bundle] || s.operationID != operationID || operationID == "" {
		return false, false
	}
	return s.accepting, s.requested
}

func (e *engine) routeStopRequested(bundle, operationID string) bool {
	_, requested := e.routeStopSnapshot(bundle, operationID)
	return requested
}

// closeRouteStop is the last safe point before committing local state.
// A request accepted before this point wins; later requests are refused.
func (e *engine) closeRouteStop(bundle, operationID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.stops[bundle]
	if s == nil || s.operationID != operationID || operationID == "" {
		return false
	}
	s.accepting = false
	return s.requested
}
