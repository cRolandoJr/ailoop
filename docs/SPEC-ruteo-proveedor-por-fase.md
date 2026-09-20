# SPEC — Ruteo de proveedor por fase

**Estado:** aprobado por el user (sesión 20-sep) · **Protocolo:** AI_LOOP §15, §18.16

## 1. El problema

El proveedor se elige una vez por corrida (precedencia de env vars en
`getLLMClient`), así que todo el loop corre con el mismo modelo. La medición del
17-sep dice que eso desperdicia en las dos puntas: el 7B local «opera bien» pero
su «juicio es flojo y varía mucho entre corridas» — las fases de juicio
(DISCOVERY/DESIGN/VERIFICATION) piden un modelo fuerte y las mecánicas uno
barato. Y el §19.1 pide un verificador INDEPENDIENTE: con los mismos pesos que
el implementador comparte sus puntos ciegos; con pesos distintos los modos de
falla se decorrelacionan.

## 2. Decisiones

| # | Decisión | Motivo |
|---|---|---|
| D-1 | `Config.Providers map[string]string` con claves `default`, `discovery`, `design`, `plan`, `implementation`, `verification` y valores `claude`, `gemini`, `openai`. Clave desconocida ⇒ **error al cargar**; valor desconocido ⇒ **error al construir** | fail-closed: un typo ignorado en silencio es el mismo modo de falla ya medido con `GEMINI_API_KEY` ensombreciendo al local |
| D-2 | Proveedor nombrado sin credencial ⇒ error que NOMBRA la variable faltante. Jamás fallback silencioso | ídem D-1; el síntoma del fallback es «el agente no sabe usar herramientas» y cuesta una sesión |
| D-3 | Sin bloque `providers` ⇒ conducta actual intacta. Con bloque: `default` manda para las fases no listadas; sin `default`, las no listadas usan la precedencia por env de hoy | retrocompat: los `.ailoop/config.json` existentes cargan igual |
| D-4 | La construcción de cada proveedor se muda a `llm.FromName(name)`; `getLLMClient` delega | una sola fuente por proveedor, incluida su política de retry (claude sin `WithRetry` porque su SDK reintenta; el resto con `WithRetry`) |
| D-5 | El crítico usa el cliente de la fase que critica (`advance.go:334` pasa a `clientFor`) | la crítica es juicio SOBRE esa fase; y elimina el último call site con `l.client` crudo en el camino de fases |
| D-6 | `Capabilities()` computa los grants de cada fase con las capacidades del cliente RUTEADO y reporta su proveedor (`PhaseGrants.Provider/Model`) | la visión difiere entre proveedores: computar grants con el default MIENTE — ofrecería `fs.read_image` a una fase cuyo modelo no ve |

## 3. Criterios de aceptación

| # | Caso | Esperado |
|---|---|---|
| AC-1 | Config sin `providers` | cliente por env, idéntico a hoy |
| AC-2 | `providers` con clave desconocida (`"verfication"`) | `Load` devuelve error que la nombra |
| AC-3 | `FromName("claude")` sin `ANTHROPIC_API_KEY` | error que nombra la variable; cero fallback |
| AC-4 | Loop ruteado: fase listada usa SU cliente, no listada el default | test con fakes distinguibles |
| AC-5 | `Capabilities()` ruteado | proveedor por fase + grants según las caps de ESE cliente (vision solo donde el cliente ve) |

Mutantes: quitar la validación de claves → AC-2 · fallback en `FromName` → AC-3 ·
`clientFor` ignorando el mapa → AC-4 · caps del default en `Capabilities` → AC-5.

## 4. Fuera de alcance (diferido con gatillo)

Doble corrida del juez débil con divergencia⇒UNKNOWN y salida estructurada por
capacidad (gatillo: la implementación del verificador independiente, que es
quien las consume — `docs/SPEC-verificador-independiente.md`). Hooks
allow/deny/ask (§18.15: diseño propio). Vision en el adaptador OpenAI y caching
de Gemini (gatillo: primer uso real de cada uno).
