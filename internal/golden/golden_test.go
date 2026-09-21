package golden

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func escribirCaso(t *testing.T, ws, nombre string, c Case) {
	t.Helper()
	if err := os.MkdirAll(CasesDir(ws), 0o755); err != nil {
		t.Fatalf("creando el directorio de casos: %v", err)
	}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("serializando el caso: %v", err)
	}
	if err := os.WriteFile(filepath.Join(CasesDir(ws), nombre+".json"), data, 0o644); err != nil {
		t.Fatalf("escribiendo el caso: %v", err)
	}
}

// --- Las tres formas de verde vacuo. Son las que justifican el paquete: un
// --- harness que dice "pasó" sin haber medido nada contesta mal la pregunta,
// --- que es peor que dejarla abierta.

func TestUnCasoSinChecksEsUnErrorDeCarga(t *testing.T) {
	ws := t.TempDir()
	escribirCaso(t, ws, "vacio", Case{Slug: "vacio", Task: "hacer algo"})

	if _, err := LoadCases(ws); err == nil {
		t.Fatal("un caso sin checks reportaría verde sin medir nada; tiene que fallar al cargar")
	}
}

func TestUnCheckQueNoAfirmaNadaEsUnErrorDeCarga(t *testing.T) {
	ws := t.TempDir()
	escribirCaso(t, ws, "mudo", Case{Slug: "mudo", Task: "t", Checks: []Check{{}}})

	if _, err := LoadCases(ws); err == nil {
		t.Fatal("un check vacío pasaría siempre; tiene que fallar al cargar")
	}
}

func TestUnaSuiteVaciaNoEsVerde(t *testing.T) {
	r := Execute(context.Background(), nil, func(context.Context, Case) (string, error) {
		t.Fatal("no debería producirse nada")
		return "", nil
	})
	if r.AllPassed() {
		t.Fatal("«no corrió nada» nunca se lee como «todo está bien»")
	}
}

// --- Carga

func TestUnCheckConDosAfirmacionesEsAmbiguoYFalla(t *testing.T) {
	ws := t.TempDir()
	escribirCaso(t, ws, "ambiguo", Case{
		Slug: "ambiguo", Task: "t",
		Checks: []Check{{Contains: "a", Regex: "b"}},
	})
	if _, err := LoadCases(ws); err == nil {
		t.Fatal("dos afirmaciones en un check dejan el veredicto sin definir")
	}
}

func TestUnaRegexRotaSeRechazaAlCargarNoAlCorrer(t *testing.T) {
	ws := t.TempDir()
	escribirCaso(t, ws, "regex", Case{Slug: "regex", Task: "t", Checks: []Check{{Regex: "("}}})
	if _, err := LoadCases(ws); err == nil {
		t.Fatal("una regex que no compila tiene que morir al cargar, no a mitad de la corrida")
	}
}

func TestDosCasosConElMismoSlugSonUnError(t *testing.T) {
	ws := t.TempDir()
	escribirCaso(t, ws, "a", Case{Slug: "repetido", Task: "t", Checks: []Check{{Contains: "x"}}})
	escribirCaso(t, ws, "b", Case{Slug: "repetido", Task: "t", Checks: []Check{{Contains: "y"}}})
	if _, err := LoadCases(ws); err == nil {
		t.Fatal("dos casos con el mismo slug hacen ilegible el histórico")
	}
}

func TestSinDirectorioDeCasosNoHayErrorPeroTampocoCasos(t *testing.T) {
	cases, err := LoadCases(t.TempDir())
	if err != nil {
		t.Fatalf("un proyecto sin casos no es un error: %v", err)
	}
	if len(cases) != 0 {
		t.Fatalf("esperaba cero casos, hay %d", len(cases))
	}
}

// --- Evaluación: determinista y sin red

func TestLosTresChecksSeEvaluanSobreElTexto(t *testing.T) {
	out := "El gate encontró el defecto en internal/state/spend.go:27"

	casos := []struct {
		nombre   string
		check    Check
		esperado bool
	}{
		{"contains presente", Check{Contains: "spend.go:27"}, true},
		{"contains ausente", Check{Contains: "runner.go"}, false},
		{"not_contains cumple", Check{NotContains: "PASS"}, true},
		{"not_contains viola", Check{NotContains: "defecto"}, false},
		{"regex matchea", Check{Regex: `spend\.go:\d+`}, true},
		{"regex no matchea", Check{Regex: `^PASS`}, false},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			ok, res := Evaluate(out, []Check{c.check})
			if ok != c.esperado {
				t.Errorf("%s: esperaba %v, dio %v", c.check.Describe(), c.esperado, ok)
			}
			if len(res) != 1 || res[0].Passed != c.esperado {
				t.Errorf("el detalle por check no coincide con el veredicto: %+v", res)
			}
		})
	}
}

func TestUnSoloCheckRojoTumbaElCaso(t *testing.T) {
	ok, res := Evaluate("hola", []Check{{Contains: "hola"}, {Contains: "chau"}})
	if ok {
		t.Fatal("con un check en rojo el caso no puede pasar")
	}
	if len(res) != 2 || !res[0].Passed || res[1].Passed {
		t.Fatalf("el detalle debe decir CUÁL falló: %+v", res)
	}
}

// --- Corrida

func TestUnCasoQueNoPudoCorrerNoCuentaComoPasado(t *testing.T) {
	cases := []Case{{Slug: "roto", Task: "t", Checks: []Check{{Contains: "x"}}}}
	r := Execute(context.Background(), cases, func(context.Context, Case) (string, error) {
		return "", errors.New("el proveedor no contestó")
	})
	if r.Total != 1 {
		t.Fatalf("un caso que no corrió sigue contando en el total: %d", r.Total)
	}
	if r.Passed != 0 || r.AllPassed() {
		t.Fatal("BLOCKED no es PASS")
	}
	if r.Results[0].Err == "" {
		t.Error("el resultado tiene que decir por qué no corrió")
	}
}

func TestUnCasoDeshabilitadoNoSeCorre(t *testing.T) {
	cases := []Case{
		{Slug: "vivo", Task: "t", Checks: []Check{{Contains: "ok"}}},
		{Slug: "parkeado", Task: "t", Checks: []Check{{Contains: "ok"}}, Disabled: true},
	}
	r := Execute(context.Background(), cases, func(_ context.Context, c Case) (string, error) {
		if c.Slug == "parkeado" {
			t.Error("no debería correrse un caso deshabilitado")
		}
		return "ok", nil
	})
	if r.Total != 1 || !r.AllPassed() {
		t.Fatalf("esperaba 1 caso verde, dio total=%d passed=%d", r.Total, r.Passed)
	}
}

func TestElResultadoGuardaElTextoYElNombreDelCaso(t *testing.T) {
	cases := []Case{{Slug: "s", Title: "un título", Task: "t", Checks: []Check{{Contains: "ok"}}}}
	r := Execute(context.Background(), cases, func(context.Context, Case) (string, error) {
		return "todo ok", nil
	})
	got := r.Results[0]
	if got.Title != "un título" || got.Output != "todo ok" {
		t.Fatalf("el resultado tiene que ser legible sin el caso original: %+v", got)
	}
}

// Los casos de ESTE repo tienen que cargar. Es el consumidor más barato de
// LoadCases: un caso mal escrito se caza acá, sin gastar una llamada al modelo
// ni esperar a que alguien corra la suite.
func TestLosCasosDeEsteRepoCargan(t *testing.T) {
	cases, err := LoadCases("../..")
	if err != nil {
		t.Fatalf("los casos versionados del repo no cargan: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("no se encontró ningún caso en golden/; si se movieron, este test es el que avisa")
	}
	for _, c := range cases {
		if c.Phase == "" {
			t.Errorf("caso %q sin fase", c.Slug)
		}
	}
}

// --- Persistencia

func TestLaCorridaSePersisteYSeLeeDeVuelta(t *testing.T) {
	ws := t.TempDir()
	AppendRun(ws, Run{Model: "claude-opus-5", Trigger: "manual", Total: 2, Passed: 1})
	AppendRun(ws, Run{Model: "gemini-2.5-pro", Trigger: "manual", Total: 2, Passed: 2})

	runs, err := ReadRuns(ws)
	if err != nil {
		t.Fatalf("ReadRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("esperaba 2 corridas, hay %d", len(runs))
	}
	if runs[0].Model != "claude-opus-5" || runs[1].Model != "gemini-2.5-pro" {
		t.Error("sin el modelo por corrida no se puede separar una regresión de pesos de una de prompt")
	}
}

func TestRegistrarLaCorridaNuncaRompe(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, ".ailoop"), []byte("no soy un directorio"), 0o644); err != nil {
		t.Fatalf("preparando el estorbo: %v", err)
	}
	AppendRun(ws, Run{Total: 1, Passed: 1})
}
