import { mount } from "../../testing/alpine.js";
import { assert, file, test } from "../../testing/test.js";
import { projectPicker } from "./projectPicker.js";

file("shared/presentation/projectPicker");

test("changing the project submits the picker's form", () => {
  const form = document.createElement("form");
  let submitted = 0;
  form.requestSubmit = () => submitted++;
  const { instance } = mount(projectPicker(), { el: form });
  instance.go();
  assert.equal(submitted, 1);
});
