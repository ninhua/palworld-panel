package shop

import (
	"errors"
	"net/http"
	"testing"

	"palpanel/internal/paldefender"
)

func TestClassifyPalDefenderDeliveryErrorMarksOnlyAmbiguousResultsUncertain(t *testing.T) {
	if err := classifyPalDefenderDeliveryError(paldefender.ErrRESTTimeout); !errors.Is(err, ErrDeliveryUncertain) {
		t.Fatalf("timeout err=%v want ErrDeliveryUncertain", err)
	}
	if err := classifyPalDefenderDeliveryError(&paldefender.RESTError{Status: http.StatusServiceUnavailable}); !errors.Is(err, ErrDeliveryUncertain) {
		t.Fatalf("503 err=%v want ErrDeliveryUncertain", err)
	}
	definitive := &paldefender.RESTError{Status: http.StatusConflict}
	if err := classifyPalDefenderDeliveryError(definitive); !errors.Is(err, definitive) || errors.Is(err, ErrDeliveryUncertain) {
		t.Fatalf("409 should remain a definitive failure: %v", err)
	}
}
