# Atlas Kaynak ve Bağlam Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Kaynak kimliği korunan kod grafiği ve göreve özel bağlam paketleri üretmek.

**Architecture:** Artifact deposu büyük payload'ları state dışında tutar.
Codegraph yalnızca çözümlenmiş ilişkileri kesin gösterir; agent bağlamı mevcut
talimat yükleyicisiyle sınırlı kaynak parçalarını birleştirir.

**Tech Stack:** Go stdlib AST/types, mevcut LSP client, engineering store.

**Spec:** `docs/superpowers/specs/2026-10-02-agent-platform-design.md`, A ve ortak sınırlar.

## Global Constraints

- Go 1.26.6; `CGO_ENABLED=0`, `GOEXPERIMENT=greenteagc`.
- State 1 MiB; artifact 32 MiB; koleksiyon başına 128 referans.
- Grafik 20.000 düğüm/50.000 kenar; sorgu 50 sonuç; impact 200 düğüm/derinlik 1-5.
- Retrieval payload 24 KiB/16 kaynak; mevcut prompt/context budget korunur.
- Talimat önceliği, izinler, ownership ve kaynak containment korunur.
- Commit/baseline ve test komutları ana planla aynıdır.

## Review Focus

- Farklı package'lardaki aynı isimlerden sahte kesin call edge üretilmesi.
- Cursor yeniden kullanılırken grafiğin değişmesi.
- Kısmi taramada dosyaların yanlış silinmiş sayılması.
- Artifact referansı görünürken dosyanın eksik/bozuk olması.
- Unicode ve alt dizin talimatlarının bağlam kesilmesinde kaybolması.

### Task A0: Hash ile doğrulanan artifact/record deposu

**Files:** Create `internal/engineering/artifacts.go`,
`internal/engineering/artifacts_test.go`; modify `internal/cmd/workflow.go`.

**Interfaces:** engineering içinde `ArtifactRef{Kind string, Hash string,
Version int, Size int64}` ve `Record{Revision uint64, Ref ArtifactRef}`.
Store üretir:
`PutArtifact(ctx context.Context, kind string, data []byte) (ArtifactRef, error)`;
`ReadArtifact(ctx context.Context, ref ArtifactRef) ([]byte, error)`;
`PutRecord(ctx context.Context, namespace, key string, expected uint64, data []byte, linked ...ArtifactRef) (Record, error)`;
`ReadRecord(ctx context.Context, namespace, key string) (Record, []byte, error)`;
`ListRecords(ctx context.Context, namespace string) (map[string]Record, error)`;
`CleanArtifacts(ctx context.Context) (int, error)`.
Record ayrıca `Linked []ArtifactRef` taşır; output/transcript gibi alt artifact
referansları index içinde açık kaydedilir. Cleanup hem record payload'ını hem
linked artifact'ları canlı sayar; hash metnini içerikten sezerek referans bulmaz.
Record JSON artifact türü `record`, version 1; namespace/key isimleri literal
dosya yolu olmaz, hash edilir. Namespace session veya canonical root kimliğiyle
ayrılır. Aynı root'un Windows case varyantları ortak canonical kimlik kullanır.

- [x] **Step 1:** `TestArtifactsIntegrityAndCAS` yaz: `require.Equal` aynı
  payload hash'leri; byte değiştirince `require.Error`; expected revision 0
  create, sonra 1 update, yeniden 1 update `require.Error` ve old record intact.
  `TestArtifactsBoundsAndCleanup`: 32 MiB+1 hata, 129. referans hata, linked
  artifact ve linked alt artifact cleanup sonrası mevcut, orphan silinmiş,
  cancelled context mutation 0. Bir linked ref eksik/bozuksa record yazımı hata.
- [x] **Step 2:** `go test -count=1 -p 1 -ldflags='-w -s' -timeout 3m ./internal/engineering -run TestArtifacts`
  çalıştır; yeni API eksikliği nedeniyle FAIL bekle.
- [x] **Step 3:** Atomic artifact-first/locked CAS index store uygula; private
  permissions, bounded read+hash, reference enumeration ve GC aynı global
  lock sırasını kullansın. Eksik manifest/corruption cleanup'ı durdursun.
- [x] **Step 4:** CLI `workflow artifacts-clean` ekle; açık invocation olmadan
  GC çalışmasın. Artifact path kullanıcıdan filesystem hedefi olarak alınmasın.
- [x] **Step 5:** Aynı test ve `./internal/cmd` çalıştır; exit 0 gerekir.
- [x] **Step 6:** Exact scoped diff için `feat: add verified engineering artifact storage`.

Test assertions: `require.Error(t, conflictErr)`,
`require.Equal(t, uint64(1), retained.Revision)`,
`require.Error(t, corruptedReadErr)`, `require.NoError(t, linkedReadErr)`.

### Task A1: Grafik, semboller ve bounded impact sorgusu

**Files:** Create `internal/codegraph/types.go`, `go.go`, `service.go`,
`service_test.go`, `internal/agent/tools/project_graph.go`, `project_graph_test.go`.
Modify `internal/agent/tools/project_map.go`, `internal/agent/coordinator.go`.

**Interfaces:** codegraph `Node{ID, Language, Path, Symbol, Kind, FileHash string;
StartLine, EndLine int}`; `Edge{From, To, Kind, Resolution, Origin string}`;
`CodeGraph{Root, SourceFingerprint string; Nodes []Node; Edges []Edge;
Partial bool; Gaps []string}`; `Query{Text, Cursor string; Depth int}`;
`Page{Nodes []Node; Edges []Edge; Cursor string; Partial bool; Gaps []string}`.
`Source{Path, Hash string; Content []byte}`.
`BuildGo(ctx context.Context, root string, sources []Source) (CodeGraph, error)`;
`Symbols(ctx context.Context, graph CodeGraph, q Query) (Page, error)`;
`Impact(ctx context.Context, graph CodeGraph, id string, q Query) (Page, error)`.
Artifact API A0; `Store.RefreshProject(ctx, root)` mevcut bounded kaynak keşfi.
Tools tarafında `LSPGraphProvider`:
`GraphForFile(ctx context.Context, path string) (codegraph.CodeGraph, error)`.
Mevcut manager/client adapter'ı tools içinde olur; codegraph LSP'ye bağımlı olmaz.

- [x] **Step 1:** `TestGraphResolvedCallsAndAmbiguousNames`: iki package'ta
  `Run`, local resolved call tek hedef; dynamic call kesin edge değil.
  `TestGraphPaginationRejectsStaleCursor`: ilk page <=50, graph hash değişince
  cursor hata. `TestGraphPartialRefreshRetainsUnknownFiles`: partial removed
  kanıtı üretmez. Bounds 20.001 node/50.001 edge/impact>200 partial+gap gösterir.
  `TestGraphResolvedReferencesAndImplementations` go/types kanıtı ile reference
  ve implements kenarları; kanıtsız dynamic hedef için kesin edge yok.
  `TestProjectGraphSourceFreshness` değişmiş kaynak için stale sonucu doğrular.
- [x] **Step 2:** `go test -count=1 -p 1 -ldflags='-w -s' -timeout 3m ./internal/codegraph ./internal/agent/tools -run 'TestGraph|TestProjectGraph'` çalıştır; hedef eksiklikte FAIL.
- [x] **Step 3:** AST declaration/import ve go/types çözümlenmiş çağrı,
  identifier reference ve interface implementation ilişkileri;
  unresolved import gaps; stable root/path/symbol IDs; hash-bound cursor uygula.
- [x] **Step 4:** `project_map symbols/impact/refresh_graph` bağla; mevcut
  query/refresh uyumlu kalsın. LSP graph adapter ve invoke kapsamı/timeout ekle;
  başka worktree'nin parent LSP instance'ı kullanılmasın.
- [x] **Step 5:** Paket testlerini tekrar çalıştır; existing map testleri de
  exit 0. İlk grafik üretimi ücretli provider'a gitmemeli.
- [x] **Step 6:** Exact scoped diff için `feat: add source-bound code relationship queries`.

Test assertions: `require.LessOrEqual(t, len(page.Nodes), 50)`,
`require.LessOrEqual(t, len(impact.Nodes), 200)`,
`require.Error(t, staleCursorErr)`, `require.True(t, partialGraph.Partial)`.

### Task A2: Ana ajan ve uzman için görev bağlamı

**Files:** Create `internal/agent/task_context.go`, `task_context_test.go`.
Modify `internal/agent/prompt/context.go`, `internal/agent/agent.go`,
`delivery_context.go`, `workflow_tool.go`, `agent_tool.go`, `coordinator.go`,
`internal/agent/templates/agent_contract.md.tpl`.

**Interfaces:** prompt export
`LoadScopedContext(ctx context.Context, cfg *config.ConfigStore, paths []string) ([]ContextFile, error)`
mevcut context loader'ı kullanır. Agent içinde
`ContextSource{Path, Hash, Kind, Content string; StartLine, EndLine int}`;
`ContextPacket{Root, TaskID, TaskFingerprint string; Sources []ContextSource;
ContractRefs []engineering.Record; Gaps []string; Truncated bool}`;
`buildTaskContext(ctx context.Context, task session.Todo, graph codegraph.CodeGraph) (ContextPacket, error)`
coordinator method'u. `renderTaskContext(ctx context.Context, packet ContextPacket) (string, error)`.
Main task yoksa boş TaskID, root session scope; dispatch task stable ID kullanır.
ContractRefs başlangıçta boş; C1 `buildTaskContext` içinde gerçek ref'leri sağlar.

- [x] **Step 1:** `TestTaskContextScopedInstructionsAndUnicode` yaz: nested
  instruction sırası mevcut loader ile aynı, `require.True(utf8.ValidString)`;
  serialization <=24*1024, Sources<=16, truncation açık.
  `TestTaskContextStaleAndForeignRoot` stale hash refresh veya gap, foreign root error; tool izinleri
  değişmemiş. `TestTaskContextMainAndDispatch` her çağrıda doğru task/spec kimliği.
- [x] **Step 2:** `go test -count=1 -p 1 -ldflags='-w -s' -timeout 3m ./internal/agent/prompt ./internal/agent -run 'TestTaskContext|TestContext'` çalıştır; hedef davranış FAIL.
- [x] **Step 3:** Paket seçim sırasını spec A2'ye göre uygula; bounded source
  read mevcut containment helper ile; JSON escaping ile untrusted support data
  olarak render et; global promptu ve kabul kriterlerini kesme.
- [x] **Step 4:** Ana Run ve dispatch/agent assignment bağla; parent transcript
  yerine küçük görev paketi, gaps ve source provenance aktar. Rol izinleri ve
  kalite-only sınırları aynı kalsın; prompt capability açıklamalarını güncelle.
- [x] **Step 5:** İki paketin tüm testlerini çalıştır; exit 0 ve paid calls 0.
- [x] **Step 6:** Exact scoped diff için `feat: prepare bounded task-specific agent context`.

Test assertions: `require.LessOrEqual(t, len(serialized), 24*1024)`,
`require.LessOrEqual(t, len(packet.Sources), 16)`,
`require.True(t, utf8.ValidString(serialized))`,
`require.Equal(t, session.TaskFingerprint(task), packet.TaskFingerprint)`.

## Alt proje çıkış kontrolü

- [x] A0-A2 regression sonuçlarını ana plan E1 raporu için sakla.
- [x] Paketlerdeki bütün Go değişikliklerini formatla ve `git diff --check` çalıştır.
- [x] CodeGraph/ContextPacket/ArtifactRef/Record arayüzleri sonraki planlarla aynı.

Execution closure: see docs/agent-platform-progress.md and docs/agent-platform-verification.json. Checkboxes track closure under recorded rulings; unavailable platform acceptance is not a passing runtime test. E1 aggregate RED was waived because its initial failure was a test assertion, not missing runtime behavior. The graph smoke uses the production tool fixture rather than inventing a CLI command.
