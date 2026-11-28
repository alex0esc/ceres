"use strict";

/* =====================================================================
   Greetings for the empty chat.

   pickGreeting() returns { title, sub, kind }:
     - 5 % of the time a meme / shitpost (kind "meme"),
     - on special days (Christmas, New Year, Easter, birthday) a fitting
       greeting plus a saying (kind "special"),
     - otherwise a greeting for the time of day plus a saying, which is
       sometimes about the weekday (kind "time").
   Every list holds many entries on purpose, and the same entry is never
   picked twice in a row. All texts live here, nowhere else.
   ===================================================================== */

const GREETING_LOCALE = "de-DE";
const BIRTHDAY = { month: 9, day: 5 };   // 05.09.
const MEME_CHANCE = 0.05;
const WEEKDAY_CHANCE = 0.3;

const TITLES = {
  night: ["Noch wach, Nachteule?", "Gute Nacht", "Hallo, Nachtschwärmer", "Mitternachts-Modus aktiviert", "Pssst … nicht den Mond wecken"],
  morning: ["Guten Morgen", "Moin Moin", "Guten Morgen, Sonnenschein", "Guten Start in den Tag", "Aufstehen, Strahlen!"],
  day: ["Guten Tag", "Hallo zusammen", "Schön, dass du da bist", "Servus", "Na, wie läuft's?"],
  evening: ["Guten Abend", "Schönen Feierabend", "Schönen Abend noch", "Hallo, Abendstern", "Der Abend ist da"],
};

const QUIPS = {
  night: [
    "Die Nacht ist jung – und du auch.",
    "Selbst der Mond hat schon Feierabend gemacht.",
    "Die Sterne schlafen schon, wir nicht.",
    "Mitternachtsgedanken sind die besten Gedanken.",
    "Schlaf wird überbewertet. Sagt dein innerer Nachtschwärmer.",
    "Nur du, ich und das leise Summen der Server.",
    "Wer nachts fragt, bekommt Antworten im Mondschein.",
    "Eule oder Lerche? Heute ganz klar Eule.",
    "Der frühe Vogel kann mich mal – wir sind Nachtmenschen.",
    "Es ist spät. Oder sehr früh. Egal, ich bin da.",
    "Die besten Ideen kommen, wenn alle anderen schlafen.",
    "Ein Kaffee zur Geisterstunde? Mutig. Ich bewundere das.",
  ],
  morning: [
    "Die Sonne geht auf, die Ideen auch.",
    "Der Kaffee ruft – aber zuerst ich.",
    "Frühaufsteher haben die besten Fragen.",
    "Heute wird ein guter Tag. Ich hab's im Gefühl.",
    "Neuer Tag, neues Glück, neue Tabs.",
    "Bevor die Welt erwacht, sind wir schon fleißig.",
    "Der frühe Vogel fängt den Wurm – wir fangen den Bug.",
    "Morgenstund hat Gold im Mund.",
    "Heute schon geatmet? Perfekt, dann können wir starten.",
    "Noch ein Schluck Kaffee, dann legen wir los.",
    "Alles auf Anfang, alles ist möglich.",
    "Ein frischer Tag, ein leeres Blatt, ein bereiter Agent.",
  ],
  day: [
    "Mittagshoch – Zeit für gute Ideen.",
    "Was darf es heute sein?",
    "Zwischen zwei Terminen? Perfekt.",
    "Die Sonne steht hoch, die Fragen auch.",
    "Kaffee Nummer drei? Ich zähle nicht mit.",
    "Ich bin bereit, wenn du es bist.",
    "Mach es dir bequem, der Tag gehört dir.",
    "Ein Tag wie gemalt – fehlt nur noch deine Frage.",
    "Nachmittagstief? Dagegen hilft ein guter Prompt.",
    "Heute schon etwas Verrücktes gelernt?",
    "Alles im grünen Bereich. Meistens.",
    "Fleißig wie ein Biber, nur mit weniger Holz.",
  ],
  evening: [
    "Der Abend gehört den Neugierigen.",
    "Die Sterne gehen an, wir auch.",
    "Zeit für einen Tee und gute Gedanken.",
    "Goldene Stunde – ideal für große Pläne.",
    "Der Tag war lang, die Fragen sind noch länger.",
    "Gemütlich machen, wir lassen den Tag ausklingen.",
    "Ein Abend wie ein guter Roman.",
    "Die Couch ruft. Ich auch.",
    "Dämmerung: die Stunde der Denker.",
    "Lass uns den Tag mit etwas Schönem beenden.",
    "Netflix kann warten, wir haben Wichtigeres vor.",
    "Feierabend? Nicht, wenn die gute Idee gerade kommt.",
  ],
};

const WEEKDAY_QUIPS = {
  1: ["Montag. Wir schaffen das. Irgendwie.", "Der Montag hat verloren, sobald du hier bist.", "Montagsmodus: an. Motivation: wird geladen …"],
  2: ["Dienstag – der heimliche Held der Woche.", "Dienstag: weder Anfang noch Ende, einfach nur Zeit für Gutes."],
  3: ["Bergfest! Ab jetzt geht's bergab – in die gute Richtung.", "Mittwoch: die Woche hat Halbzeit, du hast Rückenwind."],
  4: ["Donnerstag: fast Freitag, aber noch nicht ganz.", "Noch ein Tag bis zum Wochenende. Fast schon Vorfreude."],
  5: ["Freitag! Das Wochenende winkt schon von Weitem.", "Fast geschafft – nur noch ein paar Fragen bis zum Wochenende."],
  6: ["Samstag: der Tag der kleinen Abenteuer.", "Wochenende! Wer jetzt arbeitet, ist selbst schuld – aber ich helfe gern."],
  0: ["Sonntag – Zeit für Dinge, die Spaß machen.", "Sonntagsfrieden, mit Chat-Beilage."],
};

const SPECIAL = {
  christmas: {
    titles: ["Frohe Weihnachten!", "Ho ho ho!", "Fröhliche Weihnachten!", "Hohoho!", "Merry Christmas!", "Es weihnachtet sehr"],
    quips: [
      "Alle Jahre wieder – auch mit Prompt.",
      "Last Christmas … ich schenke dir meine volle Aufmerksamkeit.",
      "Schneeflöckchen, Weißröckchen – wie kann ich helfen?",
      "Stille Nacht, heilige Nacht – und ein Chat, der niemals schläft.",
      "Der Weihnachtsmann hat dir den besten Agenten gebracht.",
      "Plätzchen für den Server, Zimtsterne für dich.",
      "Oh du fröhliche, oh du selige, oh du Prompt-selige.",
      "Rentier Rudolph hat heute frei, ich nicht.",
      "Kling, Glöckchen, klingelingeling – kling, Prompt, klingeling.",
      "Lametta für alle!",
      "Nüsse knacken, Fragen knacken – ich bin dabei.",
      "Unterm Baum liegt nichts Besseres als eine gute Frage.",
    ],
  },
  newYearsEve: {
    titles: ["Guten Rutsch!", "Prosit Neujahr!", "Silvester!", "Noch ein paar Stunden …"],
    quips: [
      "Dinner for One? Same procedure as every year.",
      "Gute Vorsätze? Ich merke sie mir, du vergisst sie sowieso.",
      "Bleigießen ist out – Fragen ist in.",
      "Die Raketen warten, die Fragen auch.",
      "Feuerwerk bitte draußen. Fragen gern hier.",
      "Ein Glas Sekt auf das alte Jahr.",
      "Das alte Jahr war gut. Das nächste wird besser.",
      "Berliner gibt es bei mir leider nur als Bytes.",
    ],
  },
  newYear: {
    titles: ["Frohes neues Jahr!", "Prosit Neujahr!", "Neues Jahr, neues Glück!"],
    quips: [
      "Das Jahr ist noch frisch – wie deine Vorsätze.",
      "Noch ist alles möglich. Fang einfach an.",
      "Neues Jahr, neue Bugs, alte Liebe.",
      "Der Kater geht vorbei. Die Ideen bleiben.",
      "365 Tage voller Chancen – Nummer eins: dieser Chat.",
      "Das Beste kommt noch.",
    ],
  },
  easter: {
    titles: ["Frohe Ostern!", "Hoppel, hoppel!", "Frohes Osterfest!", "Hallo, Osterhase!"],
    quips: [
      "Ich habe ein paar gute Antworten versteckt.",
      "Eier suchen oder Fragen stellen – du hast die Wahl.",
      "Der Osterhase hat nichts gegen Prompts.",
      "Bunt wie ein Osterei: dieser Chat.",
      "Hasenstark. Wortwörtlich.",
      "Eier gibt's draußen, Ideen gibt's hier.",
      "Frühling lässt sein blaues Band wieder flattern durch die Lüfte …",
      "Das Eiersuchen überlasse ich dir, das Fragen übernehme ich.",
    ],
  },
  birthday: {
    titles: ["Alles Gute zum Geburtstag!", "Happy Birthday!", "Herzlichen Glückwunsch!", "Heute ist dein Tag!"],
    quips: [
      "Du bist heute der Mittelpunkt des Universums.",
      "Kerzen aus, Wünsche an – ich helfe beim Erfüllen.",
      "Ein Jahr älter, ein Jahr weiser, zwei Jahre cooler.",
      "Torte für dich, Bits und Bytes für mich.",
      "Heute darfst du ruhig mal etwas Ausgefallenes fragen.",
      "Dein Chat feiert mit – konfettilos.",
      "Heute gehört alles dir: der Kuchen, die Ehre, der Agent.",
      "Wünsch dir was – oder frag mich.",
    ],
  },
};

// [headline, optional second line]. Picked in 5 % of all cases, on purpose silly.
const MEMES = [
  ["trololololol", "ohohohohoh …"],
  ["Never gonna give you up", "Never gonna let you down."],
  ["Hello there.", "General Kenobi!"],
  ["Bruh.", ""],
  ["Es ist Mittwoch, meine Kerle.", ""],
  ["Ein wilder Prompt erscheint!", "Was willst du tun?"],
  ["Stonks ↗", "Dein Agent ist bullish."],
  ["Ich habe keinen Plan,", "aber sehr viel Selbstvertrauen."],
  ["Error 404: Motivation not found.", "Aber hier ist ein Chat."],
  ["Mein Name ist Hase,", "ich weiß von nichts."],
  ["AAAAAAAAAAAAAAAAAA", "(Entschuldigung. Das musste raus.)"],
  ["Skibidi Ceres Rizz", ""],
  ["Sigma Grindset:", "Prompt eingeben."],
  ["*Windows-Startsound*", ""],
  ["Sheeeeesh.", ""],
  ["Nice.", "Nice."],
  ["E.", ""],
  ["Das ist kein Bug,", "das ist ein Feature."],
  ["Mein Gehirn hat 1000 Tabs offen,", "999 davon spielen Musik."],
  ["Ich bin Gamer,", "ich brauche keinen Schlaf."],
  ["Bonk.", ""],
  ["Achievement unlocked:", "Du hast einen Chat geöffnet."],
  ["Lol.", "Lmao."],
  ["Dies ist ein Chat.", "Dies ist die Wahrheit."],
  ["Ctrl + Alt + Entf", "und dann einfach weitermachen."],
];

// Easter Sunday (anonymous Gregorian algorithm).
function easterSunday(year) {
  const a = year % 19, b = Math.floor(year / 100), c = year % 100;
  const d = Math.floor(b / 4), e = b % 4, f = Math.floor((b + 8) / 25);
  const g = Math.floor((b - f + 1) / 3), h = (19 * a + b - d - g + 15) % 30;
  const i = Math.floor(c / 4), k = c % 4, l = (32 + 2 * e + 2 * i - h - k) % 7;
  const m = Math.floor((a + 11 * h + 22 * l) / 451);
  const month = Math.floor((h + l - 7 * m + 114) / 31);
  const day = ((h + l - 7 * m + 114) % 31) + 1;
  return new Date(year, month - 1, day);
}

function specialDay(date) {
  const month = date.getMonth() + 1, day = date.getDate();
  if (month === BIRTHDAY.month && day === BIRTHDAY.day) return "birthday";
  if (month === 12 && (day === 24 || day === 25 || day === 26)) return "christmas";
  if (month === 12 && day === 31) return "newYearsEve";
  if (month === 1 && day === 1) return "newYear";
  const today = new Date(date.getFullYear(), month - 1, day);
  const fromEaster = Math.round((today - easterSunday(date.getFullYear())) / 86400000);
  if (fromEaster >= -2 && fromEaster <= 1) return "easter";   // Good Friday to Easter Monday
  return null;
}

function timeSlot(date) {
  const hour = date.getHours();
  if (hour >= 5 && hour < 11) return "morning";
  if (hour >= 11 && hour < 18) return "day";
  if (hour >= 18 && hour < 23) return "evening";
  return "night";
}

// Random entry that never repeats the previous pick of the same list.
const lastPick = {};
function pickFrom(list, key, rand) {
  let index = Math.floor(rand() * list.length);
  if (list.length > 1 && lastPick[key] === index) index = (index + 1) % list.length;
  lastPick[key] = index;
  return list[index];
}

function pickGreeting(date = new Date(), rand = Math.random) {
  if (rand() < MEME_CHANCE) {
    const meme = pickFrom(MEMES, "meme", rand);
    return { title: meme[0], sub: meme[1] || "", kind: "meme" };
  }
  const special = specialDay(date);
  if (special) {
    const set = SPECIAL[special];
    return { title: pickFrom(set.titles, special + "-t", rand), sub: pickFrom(set.quips, special + "-q", rand), kind: "special" };
  }
  const slot = timeSlot(date);
  const weekday = WEEKDAY_QUIPS[date.getDay()];
  const sub = weekday && rand() < WEEKDAY_CHANCE
    ? pickFrom(weekday, "weekday-" + date.getDay(), rand)
    : pickFrom(QUIPS[slot], "quip-" + slot, rand);
  return { title: pickFrom(TITLES[slot], "title-" + slot, rand), sub, kind: "time" };
}
