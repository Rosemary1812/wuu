package appserver

func (s *Server) notifyThreadStarted(thread Thread) error {
	return s.writeNotification(NotificationThreadStarted, ThreadStartedNotification{Thread: thread})
}

func (s *Server) notifyThreadUpdated(thread Thread) error {
	return s.writeNotification(NotificationThreadUpdated, ThreadUpdatedNotification{Thread: thread})
}

func (s *Server) notifyOutboundBatch(batch []outboundNotification) {
	for _, item := range batch {
		if params, ok := item.params.(ThreadUpdatedNotification); ok {
			_ = s.notifyThreadUpdated(params.Thread)
			continue
		}
		_ = s.writeNotification(item.method, item.params)
	}
}
