// Says what other components announce through the stream bar's polite
// region: `rail-announce` window events ("“Fix flaky test” is waiting on
// you."). The text is cleared first, so the same sentence is read again.
//
//   <span class="visually-hidden" x-data="streamAnnouncer" x-on:rail-announce.window="say" x-text="text"></span>

export const streamAnnouncer = () => () => ({
  text: "",

  say(event) {
    this.text = "";
    this.$nextTick(() => {
      this.text = event.detail;
    });
  },
});
