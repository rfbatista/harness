import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { ApiErrorDto } from "./api-error-dto";

describe("ApiErrorDto", () => {
  test("reads the wire name fix_url into its fix_url field, and writes it back as fix_url", () => {
    const dto = ApiErrorDto.fromJSON({ error: "off", code: "PRODUCT_OWNER_UNAVAILABLE", fix_url: "/server" });
    assert.equal(dto.fix_url, "/server");
    assert.deepEqual(JSON.parse(JSON.stringify(dto)), { error: "off", code: "PRODUCT_OWNER_UNAVAILABLE", fix_url: "/server" });
  });

  test("ignores a camel-cased fixUrl on the wire and has no fix_url when none is sent", () => {
    assert.equal(ApiErrorDto.fromJSON({ error: "x", code: "C", fixUrl: "/nope" }).fix_url, undefined);
    assert.deepEqual(JSON.parse(JSON.stringify(ApiErrorDto.fromJSON({ error: "x", code: "C" }))), { error: "x", code: "C" });
  });
});
