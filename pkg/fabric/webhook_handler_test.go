package fabric

import "testing"

func TestCIFailureIssueTitle(t *testing.T) {
	got := ciFailureIssueTitle("main", "829c7cd15e137d100a57090810e5520df7350179")
	want := "[ci] main check failure at 829c7cd"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
