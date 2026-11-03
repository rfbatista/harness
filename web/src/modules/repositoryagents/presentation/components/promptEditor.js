// The repository prompt editor, as members the Agents tab mixes in (see
// compose.js). A list of the repository's agents on the left, the editor on
// the right: Write | Preview, a counter against the limit, and a Save bar. The
// prompt is added to the agent's own prompt and never replaces it; the preview
// is the server's own assembly (never built here).
//
// Stale saves keep the draft: the person looks at the other version or puts
// the draft over it. Moving to another agent, or closing the editor, with
// unsaved changes is refused with a note; Discard first.

import { Codes } from "../../../../shared/domain/errors.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { CHIPS, PLACEHOLDER, PROMPT_LIMIT, barNote, counter, insertLine, notUsedNotice, ownPrompt, previewNotes } from "../../domain/promptEditor.js";

/** @param {{ gateway: import("../../domain/ports.js").RepositoryAgentsGateway }} deps */
export const promptEditorPart = ({ gateway }) => {
  let asked = 0;
  return {
    /** The agent the editor is open on; "" when it is closed. */
    editorAgent: "",
    /** @type {import("../../domain/ports.js").RepositoryPrompt | null} */
    prompt: null,
    draft: "",
    editorView: "write",
    /** @type {import("../../domain/ports.js").PromptPreview | null} */
    preview: null,
    previewLoading: false,
    showAllOwn: false,
    saving: false,
    loadingPrompt: false,
    /** @type {import("../../domain/ports.js").RepositoryPrompt | null} The other version, once a save was refused. */
    conflict: null,
    showTheirs: false,
    editorError: null,
    /** Why a move away from unsaved changes was refused. */
    keepNote: "",
    chips: CHIPS,
    placeholder: PLACEHOLDER,

    get editorOpen() {
      return this.editorAgent !== "";
    },
    get editorTitle() {
      return `Repository prompt · ${this.editorAgent} in ${this.repositoryName}`;
    },
    get editorIntro() {
      return `Extra instructions for this agent only in ${this.repositoryName}. They are added after the agent's own prompt and never replace it. They apply when the agent starts or resumes here, whether you run it or another agent spawns it. Agents can read this text but cannot change it.`;
    },
    get editorAgents() {
      return (this.tab?.agents ?? []).map((a) => ({
        name: a.name,
        current: a.name === this.editorAgent,
        ariaCurrent: a.name === this.editorAgent ? "true" : null,
        mark: a.prompt.set ? "set" : "",
        state: a.state === "assigned" ? "" : a.state === "unassigned" ? "unassigned" : "not enabled",
      }));
    },
    get dirty() {
      return this.prompt !== null && this.draft !== this.prompt.content;
    },
    get count() {
      return counter(this.draft.length, this.prompt?.limit ?? PROMPT_LIMIT);
    },
    get countInvalid() {
      return this.count.over ? "true" : null;
    },
    get countTone() {
      return this.count.over ? "danger" : null;
    },
    get ownToggleText() {
      return this.showAllOwn ? "Collapse" : "Show all";
    },
    get canSave() {
      return this.dirty && !this.count.over && !this.saving && this.conflict === null && !this.loadingPrompt;
    },
    get barText() {
      return barNote({ dirty: this.dirty, conflict: this.conflict !== null, over: this.count.over, version: this.prompt?.version ?? "", current: this.conflict?.version });
    },
    get notUsed() {
      const row = this.tab?.agents.find((a) => a.name === this.editorAgent);
      return notUsedNotice(row, this.repositoryName, this.tab?.project.name ?? "");
    },
    get previewView() {
      if (!this.preview) return null;
      const notes = previewNotes(this.editorAgent, this.repositoryName, this.preview.repositorySection);
      return { own: ownPrompt(this.preview.agentPrompt, this.showAllOwn), section: this.preview.repositorySection, ...notes };
    },
    get isWrite() {
      return this.editorView === "write";
    },
    get isPreview() {
      return this.editorView === "preview";
    },
    get catalogHref() {
      return `/agents/${encodeURIComponent(this.editorAgent)}/edit`;
    },
    /** The Instructions & rules tab, for rules every agent in the repository follows. */
    get instructionsHref() {
      return this.layersHref.replace(/\/layers$/, "");
    },
    get savedNote() {
      return this.prompt && this.prompt.version !== "" ? "saved" : "not saved yet";
    },

    /** Opens the editor on an agent, reading its prompt. */
    async openPrompt(agent) {
      if (agent === this.editorAgent) return;
      if (this.dirty) {
        this.keepNote = "You have unsaved changes. Save or discard them before opening another agent.";
        return;
      }
      this.keepNote = "";
      this.editorAgent = agent;
      this.prompt = null;
      this.draft = "";
      this.editorView = "write";
      this.preview = null;
      this.conflict = null;
      this.showTheirs = false;
      this.editorError = null;
      this.loadingPrompt = true;
      try {
        const p = await gateway.getRepositoryPrompt(this.repositoryId, agent);
        if (this.editorAgent !== agent) return;
        this.prompt = p;
        this.draft = p.content;
      } catch (err) {
        if (this.editorAgent === agent) this.editorError = describeError(err);
      } finally {
        if (this.editorAgent === agent) this.loadingPrompt = false;
      }
    },

    closeEditor() {
      if (this.dirty) {
        this.keepNote = "You have unsaved changes. Save or discard them before leaving the editor.";
        return;
      }
      this.keepNote = "";
      this.editorAgent = "";
      this.prompt = null;
    },

    discard() {
      this.keepNote = "";
      this.draft = this.prompt?.content ?? "";
    },

    /** One chip's example line, at the cursor of the text area (at the end without one). */
    insertChip(key) {
      const line = CHIPS.find((c) => c.key === key)?.line;
      if (!line) return;
      const area = this.$refs.promptText;
      const start = area?.selectionStart ?? this.draft.length;
      const end = area?.selectionEnd ?? start;
      const next = insertLine(this.draft, start, end, line);
      this.draft = next.text;
      this.$nextTick?.(() => {
        if (!area) return;
        area.focus();
        area.setSelectionRange(next.cursor, next.cursor);
      });
    },

    async setEditorView(view) {
      this.editorView = view;
      if (view === "preview") await this.loadPreview();
    },

    /** The server's preview of the draft; an answer that came after a newer ask is dropped. */
    async loadPreview() {
      const mine = ++asked;
      this.previewLoading = true;
      this.showAllOwn = false;
      try {
        const p = await gateway.previewRepositoryPrompt({ repositoryId: this.repositoryId, agent: this.editorAgent, content: this.draft });
        if (mine === asked) this.preview = p;
      } catch (err) {
        if (mine === asked) this.editorError = describeError(err);
      } finally {
        if (mine === asked) this.previewLoading = false;
      }
    },

    toggleOwn() {
      this.showAllOwn = !this.showAllOwn;
    },

    async savePrompt() {
      if (!this.canSave) return;
      this.saving = true;
      this.editorError = null;
      try {
        const saved = await gateway.saveRepositoryPrompt({ repositoryId: this.repositoryId, agent: this.editorAgent, content: this.draft, version: this.prompt.version });
        this.prompt = saved;
        this.draft = saved.content;
        this.keepNote = "";
        await this.load();
      } catch (err) {
        if (err?.code === Codes.REPOSITORY_PROMPT_CHANGED && err.details?.current) {
          this.conflict = err.details.current;
          this.showTheirs = false;
        } else {
          this.editorError = describeError(err);
        }
      } finally {
        this.saving = false;
      }
    },

    showTheirVersion() {
      this.showTheirs = !this.showTheirs;
    },

    /** Puts the draft over the version that was saved meanwhile, ready to save again. */
    reapplyMine() {
      if (!this.conflict) return;
      this.prompt = this.conflict;
      this.conflict = null;
      this.showTheirs = false;
    },

    /** The page feed says this agent's prompt may have changed (ours, or another person's). */
    async promptMayHaveChanged(agents) {
      if (!this.editorOpen || this.saving || !agents.includes(this.editorAgent)) return;
      const agent = this.editorAgent;
      try {
        const current = await gateway.getRepositoryPrompt(this.repositoryId, agent);
        if (this.editorAgent !== agent || current.version === this.prompt?.version) return;
        if (this.dirty) this.conflict = current;
        else {
          this.prompt = current;
          this.draft = current.content;
        }
      } catch {
        // the next event or the person's next save will say
      }
    },
  };
};
