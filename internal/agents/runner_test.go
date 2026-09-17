package agents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cRolandoJr/ailoop/internal/llm"
	"github.com/cRolandoJr/ailoop/internal/state"
	"github.com/cRolandoJr/ailoop/internal/tools"
)

// fakeLLM replays scripted replies and records what it was asked. The seam is
// llm.Client, which is the boundary right before the network: the tool loop
// under test runs for real, only the model is doubled.
type fakeLLM struct {
	replies []string
	calls   int
	seen    [][]llm.Message
	// caps lets a test say what the doubled model can do, which is how the
	// capability gate gets exercised without a real provider.
	caps llm.Capabilities
}

func (f *fakeLLM) Describe() llm.Capabilities { return f.caps }

// GenerateStream reusa Generate y entrega el texto de una: el doble no
// necesita simular el streaming, sino cumplir el contrato del puerto. Si un
// test necesitara los chunks, se le agrega ahi.
func (f *fakeLLM) GenerateStream(ctx context.Context, messages []llm.Message, onChunk func(string)) (llm.Response, error) {
	resp, err := f.Generate(ctx, messages)
	if err == nil && onChunk != nil && resp.Text != "" {
		onChunk(resp.Text)
	}
	return resp, err
}

func (f *fakeLLM) Generate(ctx context.Context, messages []llm.Message) (llm.Response, error) {
	f.seen = append(f.seen, messages)
	if f.calls >= len(f.replies) {
		return llm.Response{Text: "no more scripted replies"}, nil
	}
	r := f.replies[f.calls]
	f.calls++
	// Un uso sintetico pero distinto de cero, para que el ledger se pueda
	// verificar sin un proveedor real.
	return llm.Response{
		Text:  r,
		Usage: llm.Usage{InputTokens: 100, OutputTokens: 20},
	}, nil
}

func pedir(cap, arg string) string {
	return "<<TOOL>>\n" + cap + ": " + arg + "\n<<END>>"
}

func TestElAgenteLeeElArchivoRealAntesDeResponder(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "objetivo.go"), []byte("func Real() int { return 42 }\n"), 0644); err != nil {
		t.Fatal(err)
	}

	f := &fakeLLM{replies: []string{
		pedir("fs.read", "objetivo.go"),
		"La funcion Real devuelve 42.",
	}}

	s := state.NewState("mirar el codigo")
	out, err := RunPhase(context.Background(), s, "", f, ws, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("RunPhase: %v", err)
	}
	if out != "La funcion Real devuelve 42." {
		t.Errorf("salida = %q", out)
	}
	if f.calls != 2 {
		t.Errorf("el modelo fue llamado %d veces, quiero 2 (pedido + respuesta)", f.calls)
	}

	// La observacion real tiene que haber llegado al contexto del modelo.
	ultima := f.seen[len(f.seen)-1]
	var contexto string
	for _, m := range ultima {
		contexto += m.Content
	}
	if !strings.Contains(contexto, "return 42") {
		t.Errorf("el contenido real del archivo nunca llego al modelo")
	}
}

func TestElRechazoDePermisoLlegaAlModelo(t *testing.T) {
	// Discovery no tiene cmd.run: el agente pide, el workflow niega, y el
	// modelo se entera en vez de quedarse esperando.
	ws := t.TempDir()
	f := &fakeLLM{replies: []string{
		pedir("cmd.run", "test"),
		"No pude correr los tests, lo digo explicitamente.",
	}}

	s := state.NewState("tarea")
	s.CurrentPhase = state.PhaseDiscovery

	var vistos []tools.Result
	_, err := RunPhase(context.Background(), s, "", f, ws,
		map[string]string{"test": "echo hola"}, nil, nil,
		func(r tools.Result) { vistos = append(vistos, r) })
	if err != nil {
		t.Fatalf("RunPhase: %v", err)
	}

	if len(vistos) != 1 || vistos[0].Allowed {
		t.Fatalf("el pedido de cmd.run no fue rechazado: %+v", vistos)
	}

	ultima := f.seen[len(f.seen)-1]
	var contexto string
	for _, m := range ultima {
		contexto += m.Content
	}
	if !strings.Contains(contexto, "REFUSED") {
		t.Errorf("el rechazo no se le informo al modelo")
	}
	if strings.Contains(contexto, "hola") {
		t.Errorf("el comando se ejecuto pese al rechazo")
	}
}

func TestElVerificadorSiPuedeCorrerLosChecksDeclarados(t *testing.T) {
	ws := t.TempDir()
	f := &fakeLLM{replies: []string{
		pedir("cmd.run", "test"),
		"Los tests pasaron.",
	}}

	s := state.NewState("tarea")
	s.CurrentPhase = state.PhaseVerification

	_, err := RunPhase(context.Background(), s, "", f, ws,
		map[string]string{"test": "echo TESTS-OK"}, nil, nil, nil)
	if err != nil {
		t.Fatalf("RunPhase: %v", err)
	}

	ultima := f.seen[len(f.seen)-1]
	var contexto string
	for _, m := range ultima {
		contexto += m.Content
	}
	if !strings.Contains(contexto, "TESTS-OK") {
		t.Errorf("la salida real del comando no llego al verificador")
	}
}

func TestElLoopDeHerramientasTieneTecho(t *testing.T) {
	// Un modelo que pide un archivo mas para siempre nunca produce nada.
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "x.go"), []byte("package x\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var replies []string
	for i := 0; i < MaxToolRounds+5; i++ {
		replies = append(replies, pedir("fs.read", "x.go"))
	}
	f := &fakeLLM{replies: replies}

	s := state.NewState("tarea")
	if _, err := RunPhase(context.Background(), s, "", f, ws, nil, nil, nil, nil); err != nil {
		t.Fatalf("RunPhase: %v", err)
	}

	if f.calls > MaxToolRounds+2 {
		t.Errorf("el modelo fue llamado %d veces, el techo es %d", f.calls, MaxToolRounds)
	}
}

func TestLasDecisionesAprobadasEntranAlContexto(t *testing.T) {
	f := &fakeLLM{replies: []string{"ok"}}

	s := state.NewState("tarea")
	s.Record.Add("el test es sintetico", "determinismo", []string{"E-03"}, state.PhaseDiscovery)

	if _, err := RunPhase(context.Background(), s, "", f, t.TempDir(), nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	var contexto string
	for _, m := range f.seen[0] {
		contexto += m.Content
	}
	if !strings.Contains(contexto, "el test es sintetico") {
		t.Errorf("la decision aprobada no llego al agente:\n%s", contexto)
	}
	if !strings.Contains(contexto, "MUST NOT contradict") {
		t.Errorf("el agente no fue instruido de no contradecir las decisiones")
	}
}

func TestSinVisionNoSeOfreceLeerImagenes(t *testing.T) {
	// Ofrecer fs.read_image a un modelo que no ve produce un agente
	// describiendo con seguridad una imagen que nunca recibio.
	ciego := &fakeLLM{replies: []string{"ok"}, caps: llm.Capabilities{Vision: llm.Unsupported}}
	s := state.NewState("tarea")

	if _, err := RunPhase(context.Background(), s, "", ciego, t.TempDir(), nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	var sys string
	for _, m := range ciego.seen[0] {
		if m.Role == "system" {
			sys += m.Content
		}
	}
	if strings.Contains(sys, "fs.read_image") {
		t.Errorf("le ofrecio vision a un modelo que no la tiene:\n%s", sys)
	}
	if !strings.Contains(sys, "fs.grep") {
		t.Errorf("no le ofrecio las capacidades que si tiene")
	}
}

func TestCapacidadDesconocidaSeTrataComoAusente(t *testing.T) {
	// Fail-closed: Unknown no es permiso.
	incierto := &fakeLLM{replies: []string{"ok"}, caps: llm.Capabilities{Vision: llm.Unknown}}
	s := state.NewState("tarea")

	if _, err := RunPhase(context.Background(), s, "", incierto, t.TempDir(), nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	var sys string
	for _, m := range incierto.seen[0] {
		if m.Role == "system" {
			sys += m.Content
		}
	}
	if strings.Contains(sys, "fs.read_image") {
		t.Errorf("trato Unknown como permiso:\n%s", sys)
	}
}

func TestConVisionSeOfreceYLaImagenLlegaAlModelo(t *testing.T) {
	ws := t.TempDir()
	// PNG minimo valido (firma + IHDR truncado alcanza: solo se adjunta).
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 13}
	if err := os.WriteFile(filepath.Join(ws, "captura.png"), png, 0644); err != nil {
		t.Fatal(err)
	}

	vidente := &fakeLLM{
		replies: []string{pedir("fs.read_image", "captura.png"), "Vi la imagen."},
		caps:    llm.Capabilities{Vision: llm.Supported},
	}
	s := state.NewState("mirar una captura")

	if _, err := RunPhase(context.Background(), s, "", vidente, ws, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	ultima := vidente.seen[len(vidente.seen)-1]
	var adjuntas int
	for _, m := range ultima {
		adjuntas += len(m.Images)
	}
	if adjuntas != 1 {
		t.Errorf("llegaron %d imagenes al modelo, quiero 1", adjuntas)
	}
}

func TestElLedgerRegistraCadaLlamadaDelToolLoop(t *testing.T) {
	// Sin esto, una optimizacion de tokens no se puede defender con un
	// numero: solo con una opinion.
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "x.go"), []byte("package x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	f := &fakeLLM{replies: []string{
		pedir("fs.read", "x.go"),
		pedir("fs.read", "x.go"),
		"listo",
	}}

	s := state.NewState("tarea")
	if _, err := RunPhase(context.Background(), s, "", f, ws, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	ps, ok := s.Spend.Phases[state.PhaseDiscovery]
	if !ok {
		t.Fatalf("no se registro gasto para la fase")
	}
	if ps.Calls != 3 {
		t.Errorf("Calls = %d, quiero 3 (dos pedidos de herramienta + la respuesta)", ps.Calls)
	}
	if ps.Usage.InputTokens != 300 {
		t.Errorf("InputTokens = %d, quiero 300", ps.Usage.InputTokens)
	}
}

func TestAlImplementadorSeLeAvisaQueLeaAntesDeParchear(t *testing.T) {
	// Una regla que se aplica pero no se comunica solo produce reintentos.
	f := &fakeLLM{replies: []string{"ok"}}
	s := state.NewState("tarea")
	s.CurrentPhase = state.PhaseImplementation

	if _, err := RunPhase(context.Background(), s, "", f, t.TempDir(), nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	var sys string
	for _, m := range f.seen[0] {
		if m.Role == "system" {
			sys += m.Content
		}
	}
	if !strings.Contains(sys, "fs.read BEFORE") {
		t.Errorf("no le avisa que lea antes de parchear:\n%s", sys)
	}
}
