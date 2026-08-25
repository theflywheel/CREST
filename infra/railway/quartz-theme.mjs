// Retheme Quartz (pinned v4.5.2) to the CREST journey apps' design system
// (apps/shared/crest.css — DIGIT palette, Roboto/Roboto Mono). Ordered
// first-occurrence replacement, because two defaults — tertiary "#84a59d"
// and the rgba highlight — appear in BOTH mode blocks and need different
// CREST values per mode; lightMode precedes darkMode in the config, so the
// first hit is always the light one. Fails loudly on any miss, so a Quartz
// version drift cannot ship the stock theme silently.
import { readFileSync, writeFileSync } from "node:fs";
const path = "quartz.config.ts";
let src = readFileSync(path, "utf8");
const pairs = [
  // typography (Quartz pulls these from Google Fonts by name)
  ['"Schibsted Grotesk"', '"Roboto"'],
  ['"Source Sans Pro"', '"Roboto"'],
  ['"IBM Plex Mono"', '"Roboto Mono"'],
  // lightMode
  ['"#faf8f8"', '"#FAFAFA"'],
  ['"#e5e5e5"', '"#EEEEEE"'],
  ['"#b8b8b8"', '"#C5C5C5"'],
  ['"#4e4e4e"', '"#787878"'],
  ['"#2b2b2b"', '"#363636"'],
  ['"#284b63"', '"#C84C0E"'],
  ['"#84a59d"', '"#0B4B66"'],                     // 1st occurrence: lightMode tertiary
  ['"rgba(143, 159, 169, 0.15)"', '"rgba(200, 76, 14, 0.08)"'], // 1st: lightMode highlight
  ['"#fff23688"', '"#FBEEE8"'],
  // darkMode
  ['"#161618"', '"#14191C"'],
  ['"#393639"', '"#2C3A40"'],
  ['"#646464"', '"#5A6B72"'],
  ['"#d4d4d4"', '"#C5C5C5"'],
  ['"#ebebec"', '"#EEEEEE"'],
  ['"#7b97aa"', '"#E8752F"'],
  ['"#84a59d"', '"#7FB3CC"'],                     // 2nd occurrence: darkMode tertiary
  ['"rgba(143, 159, 169, 0.15)"', '"rgba(232, 117, 47, 0.12)"'], // 2nd: darkMode highlight
  ['"#b3aa0288"', '"#4A2610"'],
];
for (const [from, to] of pairs) {
  const i = src.indexOf(from);
  if (i < 0) throw new Error(`quartz-theme: default not found: ${from}`);
  src = src.slice(0, i) + to + src.slice(i + from.length);
}
writeFileSync(path, src);
console.log("quartz-theme: CREST theme applied");
