package reconcile

import "testing"

func TestReportHasAnomalies(t *testing.T) {
	for _, tc := range []struct {
		name   string
		report Report
		want   bool
	}{
		{name: "empty", want: false},
		{name: "pending payment", report: Report{PendingPaymentEvents: 1}, want: true},
		{name: "payment conflict", report: Report{PaymentConflicts: 1}, want: true},
		{name: "paid without delivery", report: Report{PaidWithoutDelivery: 1}, want: true},
		{name: "delivery without payment", report: Report{DeliveryWithoutPaid: 1}, want: true},
		{name: "expired lease", report: Report{ExpiredLeases: 1}, want: true},
		{name: "unknown operation", report: Report{UnknownOperations: 1}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.report.HasAnomalies(); got != tc.want {
				t.Errorf("HasAnomalies() = %v, want %v", got, tc.want)
			}
		})
	}
}
