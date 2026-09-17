package agents

import (
	"testing"

	"github.com/cRolandoJr/ailoop/internal/llm"
)

// TestMedirElAhorro no valida una condicion: reporta el numero, para que una
// decision sobre tokens se pueda discutir con una medicion.
func TestMedirElAhorro(t *testing.T) {
	for _, rondas := range []int{3, 6, 10} {
		msgs := conversacionLarga(rondas)
		antes := llm.EstimateMessages(msgs)
		saved := maskOldObservations(msgs, rondas)
		despues := llm.EstimateMessages(msgs)

		pct := 0.0
		if antes > 0 {
			pct = 100 * float64(antes-despues) / float64(antes)
		}
		t.Logf("%2d rondas: %6d -> %6d tokens  (ahorro %5d, %.0f%%)",
			rondas, antes, despues, saved, pct)
	}
}
