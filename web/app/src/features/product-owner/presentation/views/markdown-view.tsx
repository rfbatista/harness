import { isSiteHref, type Block, type Inline } from "../../domain/markdown";

type TaskState = "running" | "waiting" | "done" | "idle";

const STATE_WORD: Record<TaskState, string> = { running: "in progress", waiting: "waiting on you", done: "done", idle: "not started" };

/** An answer's Markdown as elements. Text is never markup: React escapes it. */
/** `caret`: the answer is still arriving; its mark ends the last line of text. */
export function MarkdownView({ blocks, taskState, caret = false }: { blocks: readonly Block[]; taskState: (taskId: string) => TaskState; caret?: boolean }) {
  const mark = <span className="[ caret ]" aria-hidden="true" />;
  const last = blocks.length - 1;
  return (
    <>
      {blocks.map((block, i) => {
        switch (block.kind) {
          case "heading":
            return <h4 key={i}>{inline(block.inline, taskState)}{caret && i === last && mark}</h4>;
          case "paragraph":
            return <p key={i}>{inline(block.inline, taskState)}{caret && i === last && mark}</p>;
          case "list": {
            const items = block.items.map((item, n) => <li key={n}>{inline(item, taskState)}{caret && i === last && n === block.items.length - 1 && mark}</li>);
            return block.ordered ? <ol key={i}>{items}</ol> : <ul key={i}>{items}</ul>;
          }
          case "fence":
            return (
              <pre key={i}>
                <code>{block.text}</code>
                {caret && i === last && mark}
              </pre>
            );
        }
      })}
    </>
  );
}

function inline(nodes: readonly Inline[], taskState: (taskId: string) => TaskState): React.ReactNode[] {
  return nodes.map((node, i) => {
    switch (node.kind) {
      case "text":
        return node.text;
      case "strong":
        return <strong key={i}>{inline(node.inline, taskState)}</strong>;
      case "emphasis":
        return <em key={i}>{inline(node.inline, taskState)}</em>;
      case "code":
        return <code key={i}>{node.text}</code>;
      case "link":
        return (
          <a key={i} href={node.href} {...(isSiteHref(node.href) ? {} : { target: "_blank", rel: "noopener noreferrer" })}>
            {inline(node.inline, taskState)}
          </a>
        );
      case "task": {
        const state = taskState(node.taskId);
        return (
          <a key={i} className="[ task-ref ]" href={node.href}>
            <span className="[ status ]" data-state={state} data-dot-only="">
              <span className="[ visually-hidden ]">{STATE_WORD[state]}: </span>
            </span>
            <span className="[ t ]">{node.title}</span>
          </a>
        );
      }
    }
  });
}

