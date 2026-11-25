import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { inboxFilters } from "./inboxFilters.js";

file("notifications/presentation/inboxFilters");

test("changing a filter submits the inbox's form", () => {
  const form = document.createElement("form");
  let submitted = 0;
  form.requestSubmit = () => submitted++;
  const { instance } = mount(inboxFilters(), { el: form });
  instance.go();
  assert.equal(submitted, 1);
});
