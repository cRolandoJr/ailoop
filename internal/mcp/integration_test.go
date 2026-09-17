package mcp

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// Estos tests arrancan un servidor MCP de verdad y hablan el protocolo
// completo con el. La costura queda donde tiene que estar: antes del proceso
// externo, no antes del codigo bajo prueba.
func servidorDePrueba(t *testing.T) *Pool {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 no esta disponible")
	}
	p := Open(context.Background(), []Server{{
		Name:    "codegraph",
		Command: "python3",
		Args:    []string{"testdata/fake_server.py"},
	}})
	for name, err := range p.Failed() {
		t.Fatalf("no se pudo conectar a %s: %v", name, err)
	}
	t.Cleanup(p.Close)
	return p
}

func TestHandshakeYListadoDeHerramientas(t *testing.T) {
	p := servidorDePrueba(t)
	if p.Count() != 2 {
		t.Fatalf("Count = %d, quiero 2", p.Count())
	}
	cat := p.Catalog()
	if !strings.Contains(cat, "codegraph.find_code") {
		t.Errorf("catalogo sin la herramienta:\n%s", cat)
	}
	// El servidor manda una notificacion despues del initialized. Si el
	// cliente la tomara por un resultado, el listado vendria vacio o roto.
	if strings.Contains(cat, "servidor listo") {
		t.Errorf("el cliente tomo una notificacion por un resultado:\n%s", cat)
	}
}

func TestElCatalogoNoLlevaLosSchemas(t *testing.T) {
	// Es la decision de costo: el menu en el prompt, la enciclopedia a pedido.
	p := servidorDePrueba(t)
	cat := p.Catalog()
	if strings.Contains(cat, "max_results") || strings.Contains(cat, "inputSchema") {
		t.Errorf("el catalogo filtro el schema:\n%s", cat)
	}

	desc, err := p.Describe("codegraph.find_code")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(desc, "max_results") {
		t.Errorf("describe no trajo el schema:\n%s", desc)
	}
	if len(desc) <= len(cat) {
		t.Errorf("el schema completo no es mas grande que el catalogo entero (%d vs %d)", len(desc), len(cat))
	}
}

func TestLlamadaRealDevuelveElResultado(t *testing.T) {
	p := servidorDePrueba(t)
	out, err := p.Call(context.Background(), "codegraph.find_code",
		map[string]any{"query": "donde se usa Normalizar"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !strings.Contains(out, "convert.go:83") {
		t.Errorf("la respuesta del servidor no llego entera:\n%s", out)
	}
	if !strings.Contains(out, "donde se usa Normalizar") {
		t.Errorf("los argumentos no llegaron al servidor:\n%s", out)
	}
}

func TestUnErrorDelServidorSeReportaComoError(t *testing.T) {
	// isError:true no puede leerse como un resultado valido.
	p := servidorDePrueba(t)
	_, err := p.Call(context.Background(), "codegraph.find_dead_code", nil)
	if err == nil {
		t.Error("isError del servidor no se convirtio en error")
	}
}

func TestUnServidorQueNoArrancaSeReportaYNoRompeElResto(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 no esta disponible")
	}
	p := Open(context.Background(), []Server{
		{Name: "roto", Command: "no-existe-este-binario-12345"},
		{Name: "codegraph", Command: "python3", Args: []string{"testdata/fake_server.py"}},
	})
	defer p.Close()

	if _, ok := p.Failed()["roto"]; !ok {
		t.Error("el servidor roto no quedo registrado como fallido")
	}
	if p.Count() != 2 {
		t.Errorf("el servidor sano no sobrevivio a la caida del otro: Count = %d", p.Count())
	}
}

func TestFiltroDeHerramientasPorConfig(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 no esta disponible")
	}
	p := Open(context.Background(), []Server{{
		Name:    "codegraph",
		Command: "python3",
		Args:    []string{"testdata/fake_server.py"},
		Tools:   []string{"find_code"},
	}})
	defer p.Close()

	if p.Count() != 1 {
		t.Errorf("Count = %d, quiero 1 (filtrado por config)", p.Count())
	}
	if strings.Contains(p.Catalog(), "find_dead_code") {
		t.Errorf("expuso una herramienta que la config excluyo:\n%s", p.Catalog())
	}
}
