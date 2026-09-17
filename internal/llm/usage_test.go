package llm

import "testing"

func TestEstimatedEsPegajosoAlSumar(t *testing.T) {
	// Un total que mezcla medido y estimado es un estimado. Presentarlo como
	// medido seria exactamente el tipo de numero que parece evidencia.
	var total Usage
	total.Add(Usage{InputTokens: 100, OutputTokens: 50})
	if total.Estimated {
		t.Fatalf("marco como estimado una suma de numeros medidos")
	}
	total.Add(Usage{InputTokens: 10, Estimated: true})
	if !total.Estimated {
		t.Errorf("la suma con un estimado no quedo marcada como estimada")
	}
}

func TestCacheHitRate(t *testing.T) {
	u := Usage{InputTokens: 250, CacheReadTokens: 750}
	if got := u.CacheHitRate(); got < 0.74 || got > 0.76 {
		t.Errorf("CacheHitRate = %.3f, quiero ~0.75", got)
	}
	if got := (Usage{}).CacheHitRate(); got != 0 {
		t.Errorf("sin entrada, CacheHitRate = %v, quiero 0", got)
	}
}

func TestEstimateTokensCreceConElTexto(t *testing.T) {
	corto := EstimateTokens("hola")
	largo := EstimateTokens("hola mundo, esto es bastante mas largo que lo anterior")
	if largo <= corto {
		t.Errorf("la estimacion no crece: %d vs %d", corto, largo)
	}
	if EstimateTokens("") != 0 {
		t.Errorf("texto vacio no estima 0")
	}
}

func TestEstimateMessagesCuentaImagenes(t *testing.T) {
	sinImagen := EstimateMessages([]Message{{Role: "user", Content: "mira esto"}})
	conImagen := EstimateMessages([]Message{{
		Role: "user", Content: "mira esto",
		Images: []Image{{MediaType: "image/png", Data: make([]byte, 4000)}},
	}})
	if conImagen <= sinImagen+500 {
		t.Errorf("una imagen casi no sumo: %d vs %d", sinImagen, conImagen)
	}
}

func TestTotalSumaEntradaYSalida(t *testing.T) {
	if got := (Usage{InputTokens: 10, OutputTokens: 5}).Total(); got != 15 {
		t.Errorf("Total = %d, quiero 15", got)
	}
}
