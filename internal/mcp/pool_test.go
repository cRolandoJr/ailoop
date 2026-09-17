package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func poolFalso() *Pool {
	esquemaLargo, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":      map[string]any{"type": "string", "description": "the search query to run against the graph"},
			"repo":       map[string]any{"type": "string", "description": "repository path"},
			"maxResults": map[string]any{"type": "integer", "description": "how many results"},
		},
		"required": []string{"query"},
	})
	return &Pool{
		clients: map[string]*Client{
			"codegraph": {tools: []Tool{
				{Name: "find_code", Description: "Find code by natural language. Searches the indexed graph and returns matching symbols with file and line.", InputSchema: esquemaLargo},
				{Name: "find_dead_code", Description: "List functions with no callers.", InputSchema: esquemaLargo},
			}},
		},
		failed: map[string]error{},
	}
}

func TestCatalogoEsUnaLineaPorHerramienta(t *testing.T) {
	// Es la diferencia entre pagar el menu y pagar la enciclopedia en cada
	// request del tool loop.
	cat := poolFalso().Catalog()
	lineas := strings.Count(strings.TrimSpace(cat), "\n") + 1
	if lineas != 2 {
		t.Errorf("el catalogo tiene %d lineas para 2 herramientas:\n%s", lineas, cat)
	}
	if strings.Contains(cat, "maxResults") || strings.Contains(cat, "properties") {
		t.Errorf("el catalogo filtro el schema completo:\n%s", cat)
	}
	if !strings.Contains(cat, "codegraph.find_code") {
		t.Errorf("falta el nombre calificado:\n%s", cat)
	}
}

func TestDescribeSiTraeElSchema(t *testing.T) {
	out, err := poolFalso().Describe("codegraph.find_code")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "maxResults") {
		t.Errorf("describe no trajo el schema:\n%s", out)
	}
}

func TestDescribeDeAlgoInexistenteExplicaQueHay(t *testing.T) {
	_, err := poolFalso().Describe("codegraph.no_existe")
	if err == nil {
		t.Fatal("no fallo con una herramienta inexistente")
	}
	_, err = poolFalso().Describe("otroserver.x")
	if err == nil || !strings.Contains(err.Error(), "codegraph") {
		t.Errorf("el error no dice que servidores hay: %v", err)
	}
}

func TestNombreCalificadoMalFormadoSeRechaza(t *testing.T) {
	for _, malo := range []string{"sinpunto", ".empiezaconpunto", "terminaconpunto."} {
		if _, _, err := split(malo); err == nil {
			t.Errorf("acepto %q como nombre calificado", malo)
		}
	}
}

func TestFirstSentenceAcota(t *testing.T) {
	largo := strings.Repeat("palabra ", 50)
	if got := firstSentence(largo); len(got) > 120 {
		t.Errorf("no acoto: %d caracteres", len(got))
	}
	if got := firstSentence("Primera. Segunda. Tercera."); got != "Primera." {
		t.Errorf("firstSentence = %q", got)
	}
	if got := firstSentence(""); got != "(no description)" {
		t.Errorf("descripcion vacia = %q", got)
	}
}

func TestPoolVacioNoOfreceCatalogo(t *testing.T) {
	p := &Pool{clients: map[string]*Client{}, failed: map[string]error{}}
	if !p.Empty() {
		t.Error("un pool sin clientes no se reporta vacio")
	}
	if p.Catalog() != "" {
		t.Errorf("genero catalogo sin servidores: %q", p.Catalog())
	}
}
