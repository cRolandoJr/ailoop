package state

import (
	"strings"
	"testing"

	"github.com/cRolandoJr/ailoop/internal/llm"
)

func TestElLedgerSumaPorFase(t *testing.T) {
	var l Ledger
	l.Record(PhaseDiscovery, llm.Usage{InputTokens: 1000, OutputTokens: 200})
	l.Record(PhaseDiscovery, llm.Usage{InputTokens: 1500, OutputTokens: 300})
	l.Record(PhaseDesign, llm.Usage{InputTokens: 800, OutputTokens: 100})

	if l.Phases[PhaseDiscovery].Calls != 2 {
		t.Errorf("Calls en Discovery = %d, quiero 2", l.Phases[PhaseDiscovery].Calls)
	}
	if got := l.Phases[PhaseDiscovery].Usage.InputTokens; got != 2500 {
		t.Errorf("input de Discovery = %d, quiero 2500", got)
	}

	calls, total := l.Total()
	if calls != 3 || total.InputTokens != 3300 {
		t.Errorf("Total = %d llamadas / %d input, quiero 3 / 3300", calls, total.InputTokens)
	}
}

func TestUnTotalConEstimadosSeMarcaComoEstimado(t *testing.T) {
	// Es el punto de todo esto: un numero que puede estar mal y parece
	// medido es peor que no tenerlo.
	var l Ledger
	l.Record(PhaseDiscovery, llm.Usage{InputTokens: 100, OutputTokens: 10})
	l.Record(PhaseDesign, llm.Usage{InputTokens: 100, OutputTokens: 10, Estimated: true})

	_, total := l.Total()
	if !total.Estimated {
		t.Errorf("el total mezclo medido con estimado y no lo dice")
	}
	if !strings.Contains(l.Report(), "estimated") {
		t.Errorf("el reporte no avisa que hay estimaciones:\n%s", l.Report())
	}
}

func TestReporteVacioNoInventaNumeros(t *testing.T) {
	var l Ledger
	r := l.Report()
	if !strings.Contains(r, "No model calls") {
		t.Errorf("un ledger vacio no lo dice: %q", r)
	}
}

func TestElReporteRespetaElOrdenDelLoop(t *testing.T) {
	var l Ledger
	l.Record(PhaseVerification, llm.Usage{InputTokens: 1})
	l.Record(PhaseDiscovery, llm.Usage{InputTokens: 1})

	r := l.Report()
	iDisc := strings.Index(r, string(PhaseDiscovery))
	iVer := strings.Index(r, string(PhaseVerification))
	if iDisc == -1 || iVer == -1 || iDisc > iVer {
		t.Errorf("el reporte no sigue el orden del loop:\n%s", r)
	}
}

func TestHitRateEnElReporte(t *testing.T) {
	var l Ledger
	l.Record(PhaseDesign, llm.Usage{InputTokens: 250, CacheReadTokens: 750, OutputTokens: 10})
	if !strings.Contains(l.Report(), "75%") {
		t.Errorf("el hit rate no aparece:\n%s", l.Report())
	}
}
