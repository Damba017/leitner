// Languages offered for the sides of a list. The value is a BCP 47 code for
// the browser voice; "Anders" lets the user type any other code.
export const languages = [
  ["nl", "Nederlands"],
  ["en", "Engels"],
  ["es", "Spaans"],
  ["fr", "Frans"],
  ["de", "Duits"],
  ["it", "Italiaans"],
  ["pt", "Portugees"],
];

const names = new Map(languages);

// languageName is the Dutch name of code, or the code itself.
export function languageName(code) {
  return names.get(code) ?? code;
}

export function isListed(code) {
  return names.has(code);
}
