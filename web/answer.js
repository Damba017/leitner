// Answer checking and shuffling for practice. Pure functions without the
// DOM, so offline practice can use them too and testdata/answer_test.js can
// run them in jsc.

// squash puts s in one Unicode form (NFC), writes typographic quotes as plain
// ones (iOS types ’ for ') and collapses runs of whitespace; trimmed.
function squash(s) {
  return s
    .normalize("NFC")
    .replace(/[‘’‚‛′]/gu, "'")
    .replace(/[“”„‟″]/gu, '"')
    .replace(/\s+/gu, " ")
    .trim();
}

function lower(s, lang) {
  try {
    return s.toLocaleLowerCase(lang || undefined);
  } catch {
    return s.toLowerCase(); // an unknown language code
  }
}

// stripAccents drops combining marks: é → e, ñ → n. Letters of their own
// (ß, ø, ł) stay.
const stripAccents = (s) => s.normalize("NFD").replace(/\p{Mn}/gu, "").normalize("NFC");

// forms are the versions of s to compare under the list's switches. When
// punctuation doesn't count, it is both deleted and read as a space, so
// "arc-en-ciel" matches "arcenciel" as well as "arc en ciel".
function forms(s, opts, foldAccents) {
  let v = squash(s);
  if (!opts.strictCase) {
    v = lower(v, opts.lang);
  }
  if (foldAccents) {
    v = stripAccents(v);
  }
  if (opts.strictPunct) {
    return [v];
  }
  return [squash(v.replace(/\p{P}/gu, "")), squash(v.replace(/\p{P}/gu, " "))];
}

// checkAnswer compares what was typed with a side of a card. expected may
// hold several right answers separated by "/". opts: strictCase,
// strictPunct, strictAccents (true = a difference makes it wrong) and lang,
// the language of the answer (for lower case).
// Returns {correct, accentWarning, empty}: accentWarning means only the
// accents differ (right when accents don't count, wrong when they do).
export function checkAnswer(given, expected, opts) {
  if (squash(given) === "") {
    return { correct: false, accentWarning: false, empty: true };
  }
  const answers = [...expected.split("/"), expected].map(squash).filter(Boolean);
  const matches = (foldAccents) => {
    const typed = forms(given, opts, foldAccents);
    return answers.some((a) => forms(a, opts, foldAccents).some((f) => typed.includes(f)));
  };
  if (matches(false)) {
    return { correct: true, accentWarning: false, empty: false };
  }
  if (matches(true)) {
    return { correct: !opts.strictAccents, accentWarning: true, empty: false };
  }
  return { correct: false, accentWarning: false, empty: false };
}

// shuffle returns a shuffled copy of items (Fisher–Yates).
export function shuffle(items, random = Math.random) {
  const out = [...items];
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(random() * (i + 1));
    [out[i], out[j]] = [out[j], out[i]];
  }
  return out;
}

// askedDirection is the direction to ask: "both" becomes a random side.
export function askedDirection(direction, random = Math.random) {
  if (direction === "both") {
    return random() < 0.5 ? "1-2" : "2-1";
  }
  return direction;
}
