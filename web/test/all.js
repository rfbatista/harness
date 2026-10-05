// Every test module, in run order. `go run ./cmd/webbuild -test` will generate
// this list from web/src/**/*.test.js; until then, add new test files here.
// The imports-rule check (see docs/WEB.md, Enforcement) also fails when a
// *.test.js file is missing from this list.

import "../src/shared/infrastructure/api.test.js";
import "../src/shared/infrastructure/feed.test.js";
import "../src/shared/presentation/format.test.js";
import "../src/shared/presentation/components/streamStatus.test.js";
import "../src/shared/presentation/components/themeToggle.test.js";

import "../src/modules/sessions/domain/session.test.js";
import "../src/modules/sessions/infrastructure/dto.test.js";
import "../src/modules/sessions/infrastructure/gateways.test.js";
import "../src/modules/sessions/presentation/view.test.js";
import "../src/modules/sessions/presentation/pages/sessionsPage.test.js";
import "../src/modules/sessions/presentation/components/replyBox.test.js";
