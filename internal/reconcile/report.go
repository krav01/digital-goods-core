package reconcile

// Report contains read-only consistency signals for the application database.
type Report struct {
	PendingPaymentEvents int64
	PaymentConflicts     int64
	PaidWithoutDelivery  int64
	DeliveryWithoutPaid  int64
	ExpiredLeases        int64
	UnknownOperations    int64
}

// HasAnomalies reports whether the report contains work requiring attention.
func (r Report) HasAnomalies() bool {
	return r.PendingPaymentEvents > 0 || r.PaymentConflicts > 0 || r.PaidWithoutDelivery > 0 ||
		r.DeliveryWithoutPaid > 0 || r.ExpiredLeases > 0 || r.UnknownOperations > 0
}
