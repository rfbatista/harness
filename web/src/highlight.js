// The code views' syntax highlighter, on highlight.js: the one place the
// library is imported (web/third_party/highlightjs, pinned; cmd/webbuild
// aliases "highlight.js" to it). Only these grammars are bundled; any other
// language is shown as plain text.

import hljs from "highlight.js/core.js";
import go from "highlight.js/languages/go.min.js";
import javascript from "highlight.js/languages/javascript.min.js";
import typescript from "highlight.js/languages/typescript.min.js";
import xml from "highlight.js/languages/xml.min.js";
import css from "highlight.js/languages/css.min.js";
import scss from "highlight.js/languages/scss.min.js";
import less from "highlight.js/languages/less.min.js";
import json from "highlight.js/languages/json.min.js";
import markdown from "highlight.js/languages/markdown.min.js";
import yaml from "highlight.js/languages/yaml.min.js";
import ini from "highlight.js/languages/ini.min.js";
import sql from "highlight.js/languages/sql.min.js";
import bash from "highlight.js/languages/bash.min.js";
import python from "highlight.js/languages/python.min.js";
import ruby from "highlight.js/languages/ruby.min.js";
import rust from "highlight.js/languages/rust.min.js";
import java from "highlight.js/languages/java.min.js";
import kotlin from "highlight.js/languages/kotlin.min.js";
import swift from "highlight.js/languages/swift.min.js";
import cLang from "highlight.js/languages/c.min.js";
import cpp from "highlight.js/languages/cpp.min.js";
import csharp from "highlight.js/languages/csharp.min.js";
import php from "highlight.js/languages/php.min.js";
import lua from "highlight.js/languages/lua.min.js";
import protobuf from "highlight.js/languages/protobuf.min.js";
import graphql from "highlight.js/languages/graphql.min.js";
import dockerfile from "highlight.js/languages/dockerfile.min.js";
import makefile from "highlight.js/languages/makefile.min.js";
import diff from "highlight.js/languages/diff.min.js";
import plaintext from "highlight.js/languages/plaintext.min.js";

import { hljsHighlighter } from "./modules/code/infrastructure/hljs-highlighter.js";

const grammars = {
  go,
  javascript,
  typescript,
  xml,
  css,
  scss,
  less,
  json,
  markdown,
  yaml,
  ini,
  sql,
  bash,
  python,
  ruby,
  rust,
  java,
  kotlin,
  swift,
  c: cLang,
  cpp,
  csharp,
  php,
  lua,
  protobuf,
  graphql,
  dockerfile,
  makefile,
  diff,
  plaintext,
};

/** @returns {import("./modules/code/domain/ports.js").Highlighter} */
export function createHighlighter() {
  for (const [name, grammar] of Object.entries(grammars)) {
    if (!hljs.getLanguage(name)) hljs.registerLanguage(name, grammar);
  }
  return hljsHighlighter(hljs);
}
