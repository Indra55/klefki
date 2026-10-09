// Keep query eligibility aligned with core/search/relevance.go.
import manifest from "./model-manifest.json";
const stopWords = new Set(
  "a an and are as at be been being but by can could did do does for from had has have how i if in into is it its me my of on or our please show than that the their them there these they this those to was we were what when where which who why will with would you your".split(" "),
);

export function relevantQueryTerms(terms: string[]) {
  return [...new Set(terms)].filter((t) => !stopWords.has(t)).sort();
}

export function keywordEvidence(terms: string[], document: Record<string, number>) {
  let matched = 0;
  for (const term of terms) {
    if (!document[term]) continue;
    if (/\p{N}/u.test(term)) return true;
    matched++;
  }
  const minimum = terms.length >= 4 ? Math.min(3, Math.ceil(terms.length / 3)) : 1;
  return matched >= minimum;
}

export function relevantSemanticHits<T extends { score: number }>(ranked: T[]) {
  if (!ranked.length) return ranked;
  const cutoff = Math.max(manifest.searchCutoff, ranked[0].score * manifest.searchRelative);
  return ranked.filter((h) => h.score >= cutoff);
}
