import { assert, file, test } from "../../../shared/testing/test.js";
import { documentSignature } from "./documents.js";

file("tasks/domain/documents");

test("the signature is order-free and changes with any version", () => {
  const a = documentSignature([{ id: "d2", version: "v1" }, { id: "d1", version: "v1" }]);
  assert.equal(a, "d1@v1,d2@v1");
  assert.equal(documentSignature([{ id: "d1", version: "v1" }, { id: "d2", version: "v1" }]), a);
  assert.ok(documentSignature([{ id: "d1", version: "v2" }, { id: "d2", version: "v1" }]) !== a);
  assert.equal(documentSignature([]), "");
});
