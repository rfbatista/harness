import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { fakeServices, listDTO, requestDTO, serviceDTO } from "../../testing/fakeGateway.js";
import { servicesNavCount } from "./servicesNavCount.js";

file("services/presentation/components/servicesNavCount");

const navEl = (count) => ({ dataset: { projectId: "p1", runningServices: String(count) } });
const changed = (seq, running) => ({ seq, kind: "service_changed", service: serviceDTO(), running_count: running });

function open(count = 2, gateway = fakeServices()) {
  const { instance } = mount(servicesNavCount({ gateway }), { el: navEl(count) });
  instance.init();
  return { nav: instance, gateway };
}

test("it starts at the server-rendered count and follows the project's feed", () => {
  const { nav, gateway } = open(2);
  assert.equal(nav.count, 2);
  assert.deepEqual(gateway.followed, ["p1"]);
  assert.equal(nav.badgeText, "2");
  assert.equal(nav.badgeShown, true);
});

test("a feed event sets the count from its running_count", () => {
  const { nav, gateway } = open(2);
  gateway.emit(changed(5, 3));
  assert.equal(nav.count, 3);
  gateway.emit({ seq: 6, kind: "service_deleted", service_id: "s1", running_count: 0 });
  assert.equal(nav.count, 0);
  assert.equal(nav.badgeShown, false);
});

test("the item's data-running-services follows the count", () => {
  const el = navEl(2);
  const gateway = fakeServices();
  const { instance } = mount(servicesNavCount({ gateway }), { el });
  instance.init();
  gateway.emit(changed(3, 5));
  assert.equal(el.dataset.runningServices, "5");
});

test("an event older than one already seen is ignored", () => {
  const { nav, gateway } = open(2);
  gateway.emit(changed(8, 4));
  gateway.emit(changed(7, 1));
  assert.equal(nav.count, 4);
});

test("above 99 the badge reads 99+, and says running for a screen reader", () => {
  const { nav, gateway } = open(0);
  gateway.emit(changed(1, 140));
  assert.equal(nav.badgeText, "99+");
});

test("a resynced feed reads the count again from the list", async () => {
  const gateway = fakeServices({ list: listDTO([serviceDTO({ id: "a", name: "a", status: "running" }), serviceDTO({ id: "b", name: "b", status: "starting" }), serviceDTO({ id: "c", name: "c" })], { seq: 12 }) });
  const { nav } = open(0, gateway);
  gateway.status("resynced");
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(nav.count, 2);
  gateway.emit(changed(12, 9));
  assert.equal(nav.count, 2, "seq 12 is already in the list that was read");
});

test("it stops following when it goes", () => {
  const { nav, gateway } = open();
  nav.destroy();
  assert.equal(gateway.unfollowed, 1);
});

test("a service request event moves the sequence and leaves the count alone", () => {
  const { nav, gateway } = open(2);
  gateway.emit({ seq: 5, kind: "service_request", request: requestDTO() });
  assert.deepEqual([nav.count, nav.seq], [2, 5]);
  gateway.emit(changed(6, 4));
  assert.equal(nav.count, 4);
});
