import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { topBarJson } from "../../testing/fixtures";
import { NavigatedDto } from "./navigated-dto";
import { LinkDto, ProjectDto, TopBarDto } from "./top-bar-dto";

const failsAt = (path: string, dto: string) => (err: unknown) => err instanceof DtoError && err.path === path && err.dto === dto && err.message.includes(path);

describe("TopBarDto", () => {
  test("reads the contract's JSON, keeping the wire names, and round-trips through toJSON", () => {
    const json = { ...topBarJson({ path: "/agents", current_project_id: "p1" }), notifications: undefined };
    const { notifications: _ignored, ...wire } = json;
    const dto = TopBarDto.fromJSON(wire);
    assert.equal(dto.current_project_id, "p1");
    assert.equal(dto.links[0] instanceof LinkDto && dto.projects[0] instanceof ProjectDto, true);
    assert.deepEqual(JSON.parse(JSON.stringify(dto)), wire);
  });

  test("ignores unknown fields, in the model and in its parts", () => {
    const json = topBarJson({ future: 1, projects: [{ id: "p", name: "n", extra: true } as never], links: [{ key: "board", href: "/", current: false, attention: false, extra: 1 } as never] });
    assert.deepEqual(TopBarDto.fromJSON(json).toJSON().projects, [{ id: "p", name: "n" }]);
  });

  test("names the DTO and the field for a missing field", () => {
    const { path: _p, ...missing } = topBarJson();
    assert.throws(() => TopBarDto.fromJSON(missing), failsAt("path", "TopBarDto"));
  });

  test("names the path of a wrong type inside a list", () => {
    assert.throws(() => TopBarDto.fromJSON(topBarJson({ projects: [{ id: "a", name: "A" }, { id: 7, name: "B" } as never] })), failsAt("projects[1].id", "ProjectDto"));
    assert.throws(() => TopBarDto.fromJSON(topBarJson({ links: [{ key: "board", href: "/", current: "yes", attention: false } as never] })), failsAt("links[0].current", "LinkDto"));
  });

  test("refuses what is not an object, and lists that are not arrays", () => {
    for (const raw of [null, "x", 3, [], undefined]) assert.throws(() => TopBarDto.fromJSON(raw), failsAt("", "TopBarDto"));
    assert.throws(() => TopBarDto.fromJSON(topBarJson({ links: {} as never })), failsAt("links", "TopBarDto"));
  });
});

describe("NavigatedDto", () => {
  test("reads the path of the event detail and round-trips", () => {
    assert.deepEqual(NavigatedDto.fromJSON({ path: "/agents", more: 1 }).toJSON(), { path: "/agents" });
  });

  test("refuses a detail that is missing, not an object, or has no string path", () => {
    for (const raw of [undefined, null, "/agents", {}, { path: 42 }]) assert.throws(() => NavigatedDto.fromJSON(raw), (err: unknown) => err instanceof DtoError && err.dto === "NavigatedDto");
  });
});
