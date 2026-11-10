// Table tests for web/answer.js. Run by js_test.go as
// `jsc -m testdata/answer_test.js`; prints each failure and throws if any.

import { checkAnswer, shuffle, askedDirection } from "../web/answer.js";

const right = { correct: true, accentWarning: false, empty: false };
const wrong = { correct: false, accentWarning: false, empty: false };
const accentsOK = { correct: true, accentWarning: true, empty: false }; // only accents differ, allowed
const accentsWrong = { correct: false, accentWarning: true, empty: false }; // only accents differ, strict
const empty = { correct: false, accentWarning: false, empty: true };

// [name, typed, expected, switches (lenient by default, Spanish), want]
const cases = [
  ["exact", "hola", "hola", {}, right],
  ["leading and trailing spaces", "  hola \t", "hola", {}, right],
  ["double spaces count as one", "el  mes", "el mes", {}, right],
  ["spaces in the card too", "el mes", " el   mes ", {}, right],
  ["tab inside", "el\tmes", "el mes", {}, right],
  ["different word", "adiós", "hola", {}, wrong],
  ["empty", "", "hola", {}, empty],
  ["only spaces", "   ", "hola", {}, empty],

  ["first alternative", "el mes", "el mes / mes", {}, right],
  ["second alternative", "mes", "el mes / mes", {}, right],
  ["all alternatives typed", "el mes / mes", "el mes / mes", {}, right],
  ["not an alternative", "la mes", "el mes / mes", {}, wrong],
  ["part of an alternative", "el", "el mes / mes", {}, wrong],
  ["alternative without accent", "ano", "el año / año", {}, accentsOK],
  ["empty alternative is ignored", ".", "hola /", {}, wrong],

  ["case ignored", "HoLa", "hola", {}, right],
  ["case strict", "Hola", "hola", { strictCase: true }, wrong],
  ["case strict, same case", "Hola", "Hola", { strictCase: true }, right],
  ["case strict and accents differ", "Nino", "niño", { strictCase: true }, wrong],
  ["Turkish dotted capital I", "İstanbul", "istanbul", { lang: "tr" }, right],
  ["Turkish dotless i", "ISPARTA", "ısparta", { lang: "tr" }, right],
  ["German capital sharp s", "STRASSE", "straße", { lang: "de" }, wrong],
  ["German first capital", "Straße", "straße", { lang: "de" }, right],
  ["unknown language code", "Hola", "hola", { lang: "not a tag" }, right],

  ["punctuation ignored", "Que", "¿Qué?", {}, accentsOK],
  ["punctuation ignored, accents right", "qué", "¿Qué?", {}, right],
  ["punctuation ignored, accents strict", "que", "¿Qué?", { strictAccents: true }, accentsWrong],
  ["punctuation strict, missing", "Qué", "¿Qué?", { strictPunct: true }, wrong],
  ["punctuation strict, typed", "¿qué?", "¿Qué?", { strictPunct: true }, right],
  ["punctuation typed but not needed", "hola!", "hola", {}, right],
  ["only punctuation typed", "?", "hola", {}, wrong],
  ["apostrophe deleted", "cest", "c'est", { lang: "fr" }, right],
  ["apostrophe as space", "c est", "c'est", { lang: "fr" }, right],
  ["curly apostrophe, punctuation strict", "c’est", "c'est", { lang: "fr", strictPunct: true }, right],
  ["curly quotes, punctuation strict", "“oui”", '"oui"', { lang: "fr", strictPunct: true }, right],
  ["hyphens as spaces", "arc en ciel", "arc-en-ciel", { lang: "fr" }, right],
  ["hyphens deleted", "arcenciel", "arc-en-ciel", { lang: "fr" }, right],
  ["hyphens typed", "arc-en-ciel", "arc-en-ciel", { lang: "fr" }, right],
  ["hyphens strict", "arc en ciel", "arc-en-ciel", { lang: "fr", strictPunct: true }, wrong],
  ["comma inside", "si no", "si, no", {}, right],

  ["missing accent allowed", "nino", "niño", {}, accentsOK],
  ["missing accent strict", "nino", "niño", { strictAccents: true }, accentsWrong],
  ["wrong accent", "cafè", "café", { lang: "fr" }, accentsOK],
  ["extra accent", "café", "cafe", {}, accentsOK],
  ["accents right, strict", "niño", "niño", { strictAccents: true }, right],
  ["decomposed input (NFD)", "niño", "niño", { strictAccents: true }, right],
  ["decomposed card (NFD)", "niño", "niño", { strictAccents: true }, right],
  ["ø is a letter, not an accent", "o", "ø", { lang: "da" }, wrong],
  ["accent and case both off", "ÁRBOL", "árbol", {}, right],
  ["accent missing and case different", "ARBOL", "árbol", {}, accentsOK],
  ["all strict", "El Niño", "El Niño", { strictCase: true, strictPunct: true, strictAccents: true }, right],
];

const failures = [];
for (const [name, typed, expected, switches, want] of cases) {
  const opts = { strictCase: false, strictPunct: false, strictAccents: false, lang: "es", ...switches };
  const got = checkAnswer(typed, expected, opts);
  if (got.correct !== want.correct || got.accentWarning !== want.accentWarning || got.empty !== want.empty) {
    failures.push(`${name}: checkAnswer(${JSON.stringify(typed)}, ${JSON.stringify(expected)}) = ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
  }
}

// shuffle: a permutation, the input left alone, every order possible.
const items = [1, 2, 3, 4, 5];
const seq = (values) => {
  let i = 0;
  return () => values[i++ % values.length];
};
const shuffled = shuffle(items, seq([0.3, 0.9, 0.1, 0.6]));
if (JSON.stringify(items) !== "[1,2,3,4,5]") {
  failures.push(`shuffle changed its input: ${items}`);
}
if (JSON.stringify([...shuffled].sort()) !== "[1,2,3,4,5]") {
  failures.push(`shuffle lost items: ${shuffled}`);
}
if (JSON.stringify(shuffle(items, () => 0.999)) !== "[1,2,3,4,5]") {
  failures.push("shuffle with random ≈ 1 should keep the order");
}
if (JSON.stringify(shuffle(items, () => 0)) !== "[2,3,4,5,1]") {
  failures.push(`shuffle with random 0 = ${shuffle(items, () => 0)}, want [2,3,4,5,1]`);
}
if (shuffle([], Math.random).length !== 0) {
  failures.push("shuffle of nothing");
}

const dirCases = [
  ["1-2", 0.9, "1-2"],
  ["2-1", 0.1, "2-1"],
  ["both", 0.1, "1-2"],
  ["both", 0.9, "2-1"],
];
for (const [dir, r, want] of dirCases) {
  const got = askedDirection(dir, () => r);
  if (got !== want) {
    failures.push(`askedDirection(${dir}, ${r}) = ${got}, want ${want}`);
  }
}

for (const f of failures) {
  print(f);
}
if (failures.length) {
  throw new Error(`${failures.length} answer test(s) failed`);
}
