// Read aloud with the browser's voice (Web Speech API), in a list's language.

export const canSpeak = "speechSynthesis" in window && "SpeechSynthesisUtterance" in window;

// spokenText is what to say for a written side: only the first of several
// answers separated by "/" ("el mes / mes" says "el mes").
export function spokenText(text) {
  return text.split("/")[0].trim() || text;
}

// speak says text in lang, stopping whatever is being said.
export function speak(text, lang) {
  if (!canSpeak) {
    return;
  }
  speechSynthesis.cancel();
  const utterance = new SpeechSynthesisUtterance(text);
  utterance.lang = lang;
  speechSynthesis.speak(utterance);
}
