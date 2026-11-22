// reveal_node — drives reveal.mjs's wire in plain Node with stand-in
// elements. signin/revealjs_test.go is the only caller; it reads back
// the field's type after each step. Beside reveal.mjs so `go test`
// finds it by path; never embedded.
import { wire } from "./reveal.mjs";

const element = (props) => {
  const handlers = {};
  return {
    ...props,
    addEventListener(name, run) { handlers[name] = run; },
    fire(name) { handlers[name](); },
  };
};

const form = element({});
const field = element({ type: "text", form });
const box = element({ checked: true });
const row = { hidden: true };
const steps = {};

wire(box, field, row);
steps.wired = { type: field.type, checked: box.checked, rowHidden: row.hidden };
box.checked = true;
box.fire("change");
steps.on = field.type;
box.checked = false;
box.fire("change");
steps.off = field.type;
box.checked = true;
box.fire("change");
form.fire("submit");
steps.sent = field.type;

process.stdout.write(JSON.stringify(steps));
