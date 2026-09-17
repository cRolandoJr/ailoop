package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func regConTodo() *Registry {
	return &Registry{
		Allowed:      map[Capability]bool{FSRead: true, FSList: true, CmdRun: true},
		RunnableCmds: map[string]string{"test": "echo corrio-el-test"},
	}
}

func TestParseSacaVariosPedidos(t *testing.T) {
	texto := strings.Join([]string{
		"Necesito ver dos cosas.",
		"<<TOOL>>",
		"fs.read: main.go",
		"<<END>>",
		"y tambien",
		"<<TOOL>>",
		"fs.list: internal",
		"<<END>>",
	}, "\n")

	reqs := Parse(texto)
	if len(reqs) != 2 {
		t.Fatalf("Parse devolvio %d pedidos, quiero 2", len(reqs))
	}
	if reqs[0].Cap != FSRead || reqs[0].Arg != "main.go" {
		t.Errorf("req[0] = %+v", reqs[0])
	}
	if reqs[1].Cap != FSList || reqs[1].Arg != "internal" {
		t.Errorf("req[1] = %+v", reqs[1])
	}
}

func TestParseIgnoraBloquesSinCerrar(t *testing.T) {
	// Adivinar donde termina un bloque roto es inventar la intencion del modelo.
	if reqs := Parse("<<TOOL>>\nfs.read: x.go\n"); len(reqs) != 0 {
		t.Errorf("acepto un bloque sin cerrar: %+v", reqs)
	}
}

func TestSinPermisoSeRechazaAunqueSeaTecnicamentePosible(t *testing.T) {
	// El corazon de 18.16: la capacidad existe, el permiso no.
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "secreto.txt"), []byte("contenido"), 0644); err != nil {
		t.Fatal(err)
	}

	reg := &Registry{Allowed: map[Capability]bool{FSList: true}} // sin FSRead
	res := Execute(context.Background(), ws, reg, Request{Cap: FSRead, Arg: "secreto.txt"})

	if res.Allowed {
		t.Errorf("permitio fs.read sin tenerlo en la allowlist")
	}
	if strings.Contains(res.Output, "contenido") {
		t.Errorf("devolvio el contenido del archivo pese a rechazar: %q", res.Output)
	}
	if !strings.Contains(res.Output, "REFUSED") {
		t.Errorf("el rechazo no se le explica al modelo: %q", res.Output)
	}
}

func TestRegistryVacioNoPermiteNada(t *testing.T) {
	// Fail-closed: un registry sin configurar no puede ser una puerta abierta.
	res := Execute(context.Background(), t.TempDir(), &Registry{}, Request{Cap: FSRead, Arg: "x"})
	if res.Allowed {
		t.Errorf("un Registry vacio permitio una capacidad")
	}
}

func TestNoSePuedeLeerFueraDelWorkspace(t *testing.T) {
	res := Execute(context.Background(), t.TempDir(), regConTodo(), Request{Cap: FSRead, Arg: "../../../etc/passwd"})
	if !strings.Contains(res.Output, "ERROR") {
		t.Errorf("leyo fuera del workspace: %q", res.Output)
	}
}

func TestCmdRunSoloCorreLoDeclarado(t *testing.T) {
	ws := t.TempDir()

	ok := Execute(context.Background(), ws, regConTodo(), Request{Cap: CmdRun, Arg: "test"})
	if !strings.Contains(ok.Output, "corrio-el-test") {
		t.Errorf("no corrio el comando declarado: %q", ok.Output)
	}

	// Un comando que el proyecto no declaro no se ejecuta, aunque el modelo
	// lo pida con toda seguridad.
	malo := Execute(context.Background(), ws, regConTodo(), Request{Cap: CmdRun, Arg: "rm -rf /"})
	if !strings.Contains(malo.Output, "ERROR") {
		t.Errorf("acepto un comando no declarado: %q", malo.Output)
	}
}

func TestProtocolNoOfreceLoQueNoPermite(t *testing.T) {
	reg := &Registry{Allowed: map[Capability]bool{FSRead: true}}
	p := Protocol(reg)
	if !strings.Contains(p, "fs.read") {
		t.Errorf("el protocolo no menciona la capacidad permitida")
	}
	if strings.Contains(p, "cmd.run") {
		t.Errorf("el protocolo le ofrece al modelo algo que no tiene permitido:\n%s", p)
	}
}

func TestProtocolVacioSinCapacidades(t *testing.T) {
	if p := Protocol(&Registry{}); p != "" {
		t.Errorf("genero protocolo sin capacidades: %q", p)
	}
}
