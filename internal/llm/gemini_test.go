package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestElSystemPromptVaASystemInstruction(t *testing.T) {
	// Antes se mapeaba a un turno de usuario: la instruccion pasaba de ser
	// algo bajo lo que el modelo opera a algo con lo que puede discutir.
	req := buildGeminiRequest([]Message{
		{Role: "system", Content: "sos el agente de discovery"},
		{Role: "user", Content: "hace la tarea"},
	})

	if req.SystemInstruction == nil {
		t.Fatal("el system prompt no llego a systemInstruction")
	}
	if req.SystemInstruction.Parts[0].Text != "sos el agente de discovery" {
		t.Errorf("systemInstruction = %+v", req.SystemInstruction.Parts)
	}
	for _, c := range req.Contents {
		for _, p := range c.Parts {
			if strings.Contains(p.Text, "agente de discovery") {
				t.Errorf("el system prompt tambien se colo en contents")
			}
		}
	}
}

func TestVariosSystemSeAcumulan(t *testing.T) {
	// El loop arma el system en partes: fase + protocolo de tools + catalogo.
	req := buildGeminiRequest([]Message{
		{Role: "system", Content: "parte A"},
		{Role: "system", Content: "parte B"},
		{Role: "user", Content: "hola"},
	})
	if req.SystemInstruction == nil || len(req.SystemInstruction.Parts) != 2 {
		t.Fatalf("no acumulo las dos partes: %+v", req.SystemInstruction)
	}
}

func TestLasImagenesLleganComoInlineData(t *testing.T) {
	// El adaptador declaraba Vision: Supported y descartaba las imagenes.
	// El agente pedia una captura y la recibia... nadie.
	req := buildGeminiRequest([]Message{{
		Role:    "user",
		Content: "que ves aca",
		Images:  []Image{{MediaType: "image/png", Data: []byte{1, 2, 3}}},
	}})

	if len(req.Contents) != 1 {
		t.Fatalf("Contents = %d", len(req.Contents))
	}
	var tieneImagen, tieneTexto bool
	for _, p := range req.Contents[0].Parts {
		if p.InlineData != nil {
			tieneImagen = true
			if p.InlineData.MimeType != "image/png" {
				t.Errorf("mime = %q", p.InlineData.MimeType)
			}
			if p.InlineData.Data == "" {
				t.Errorf("la imagen llego vacia")
			}
		}
		if p.Text != "" {
			tieneTexto = true
		}
	}
	if !tieneImagen {
		t.Error("la imagen se perdio")
	}
	if !tieneTexto {
		t.Error("el texto se perdio")
	}
}

func TestRolesConsecutivosSeFusionan(t *testing.T) {
	// Gemini rechaza dos turnos seguidos del mismo rol, y el tool loop
	// produce exactamente eso al encadenar observaciones.
	req := buildGeminiRequest([]Message{
		{Role: "user", Content: "uno"},
		{Role: "user", Content: "dos"},
		{Role: "assistant", Content: "respuesta"},
	})

	for i := 1; i < len(req.Contents); i++ {
		if req.Contents[i].Role == req.Contents[i-1].Role {
			t.Fatalf("quedaron dos turnos seguidos del rol %q", req.Contents[i].Role)
		}
	}
	if len(req.Contents[0].Parts) != 2 {
		t.Errorf("no fusiono los dos turnos de usuario: %+v", req.Contents[0].Parts)
	}
}

func TestElRolAssistantSeMapeaAModel(t *testing.T) {
	req := buildGeminiRequest([]Message{
		{Role: "user", Content: "hola"},
		{Role: "assistant", Content: "hola"},
	})
	if req.Contents[1].Role != "model" {
		t.Errorf("rol = %q, quiero model", req.Contents[1].Role)
	}
}

func TestElJSONNoLlevaCamposVacios(t *testing.T) {
	// Un part con text y inline_data a la vez es invalido para la API.
	req := buildGeminiRequest([]Message{{
		Role:   "user",
		Images: []Image{{MediaType: "image/png", Data: []byte{9}}},
	}})
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, `"text":""`) {
		t.Errorf("serializo un text vacio junto a la imagen:\n%s", s)
	}
	if !strings.Contains(s, "inline_data") {
		t.Errorf("no serializo inline_data:\n%s", s)
	}
}
