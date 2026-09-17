package state

import (
	"errors"
	"strings"
	"testing"

	"github.com/cRolandoJr/ailoop/internal/llm"
)

func ledgerCon(in, out int) *Ledger {
	var l Ledger
	l.Record(PhaseDiscovery, llm.Usage{InputTokens: in, OutputTokens: out})
	return &l
}

func TestSinTechoNoBloqueaNada(t *testing.T) {
	if err := (Budget{}).Check(ledgerCon(1e9, 1e9)); err != nil {
		t.Errorf("bloqueo sin techo configurado: %v", err)
	}
}

func TestElTechoDeTokensCorta(t *testing.T) {
	b := Budget{MaxTokens: 1000}
	if err := b.Check(ledgerCon(400, 100)); err != nil {
		t.Errorf("corto por debajo del techo: %v", err)
	}
	err := b.Check(ledgerCon(900, 200))
	if !errors.Is(err, ErrOverBudget) {
		t.Fatalf("err = %v, quiero ErrOverBudget", err)
	}
	if !strings.Contains(err.Error(), "1000") {
		t.Errorf("el mensaje no dice cual era el techo: %v", err)
	}
}

func TestUnTechoEnDolaresSinPreciosLoDiceEnVezDeIgnorarlo(t *testing.T) {
	// Un techo que no se puede evaluar y se ignora en silencio es peor que
	// no tener techo: el usuario cree que esta protegido.
	b := Budget{MaxUSD: 0.5}
	err := b.Check(ledgerCon(10, 10))
	if !errors.Is(err, ErrOverBudget) {
		t.Fatalf("ignoro un techo que no puede evaluar: %v", err)
	}
	if !strings.Contains(err.Error(), "price_in_per_mtok") {
		t.Errorf("no dice como arreglarlo: %v", err)
	}
}

func TestElTechoEnDolaresConPrecios(t *testing.T) {
	// 1M de entrada a $5 y 1M de salida a $25.
	b := Budget{MaxUSD: 0.10, PriceInPerMTok: 5, PriceOutPerMTok: 25}

	// 10k in + 1k out = 0.05 + 0.025 = $0.075 -> por debajo
	if err := b.Check(ledgerCon(10_000, 1_000)); err != nil {
		t.Errorf("corto por debajo del techo: %v", err)
	}
	// 20k in + 2k out = 0.10 + 0.05 = $0.15 -> por encima
	err := b.Check(ledgerCon(20_000, 2_000))
	if !errors.Is(err, ErrOverBudget) {
		t.Fatalf("no corto: %v", err)
	}
}

func TestElMensajeAvisaCuandoLosNumerosSonEstimados(t *testing.T) {
	var l Ledger
	l.Record(PhaseDiscovery, llm.Usage{InputTokens: 1e6, OutputTokens: 0, Estimated: true})
	b := Budget{MaxUSD: 0.5, PriceInPerMTok: 5}

	err := b.Check(&l)
	if err == nil {
		t.Fatal("no corto")
	}
	if !strings.Contains(err.Error(), "estimated") {
		t.Errorf("presenta un estimado como medido: %v", err)
	}
}

func TestRemainingNoMienteSobreLoQueNoPuedeEvaluar(t *testing.T) {
	b := Budget{MaxUSD: 0.5} // sin precios
	got := b.Remaining(ledgerCon(10, 10))
	if !strings.Contains(got, "cannot be enforced") {
		t.Errorf("Remaining = %q, quiero que diga que no se puede aplicar", got)
	}
}
