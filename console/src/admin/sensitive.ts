/**
 * Whether a variable's name reads like a credential.
 *
 * A guess about a form's default, never about storage. Whichever kind the form
 * lands on, the person saving sees which it is and can change it — Pando does
 * not decide that a variable is a secret on the strength of its name.
 *
 * It exists because the other default is the wrong one for `ANTHROPIC_API_KEY`.
 * A plain value lives in the spec, and the spec is exportable and meant to be
 * safe to hand to somebody (design 01 §5, R-190, R-191); a secret lives in the
 * secrets adapter and the spec carries only a reference. Getting that backwards
 * once puts a key in every export of the app from then on.
 *
 * PUBLIC wins over KEY: a VAPID public key is published to browsers by design.
 */
export function looksSensitive(key: string): boolean {
  const upper = key.toUpperCase();
  if (upper.includes('PUBLIC')) return false;
  return /SECRET|PASSWORD|PASSWD|TOKEN|PRIVATE|CREDENTIAL|SALT|SIGNING|KEY/.test(upper);
}
