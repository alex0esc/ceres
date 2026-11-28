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

const GREETING_LOCALE = "en-US";
const BIRTHDAY = { month: 9, day: 5 };   // 09/05.
const MEME_CHANCE = 0.05;
const WEEKDAY_CHANCE = 0.3;

const TITLES = {
  night: ["Still up, night owl?", "Good night", "Hello, night owl", "Midnight mode activated", "Shh … don't wake the moon"],
  morning: ["Good morning", "Morning!", "Good morning, sunshine", "Great start to the day", "Rise and shine!"],
  day: ["Good day", "Hello there", "Glad you're here", "Howdy", "How's it going?"],
  evening: ["Good evening", "Enjoy the evening", "Have a lovely evening", "Hello, evening star", "The evening is here"],
};

const QUIPS = {
  night: [
    "The night is young – and so are you.",
    "Even the moon has clocked out.",
    "The stars are already asleep. We're not.",
    "Midnight thoughts are the best thoughts.",
    "Sleep is overrated. Says your inner night owl.",
    "Just you, me, and the quiet hum of the servers.",
    "Ask at night, get answers by moonlight.",
    "Owl or lark? Clearly an owl tonight.",
    "The early bird can wait – we're night people.",
    "It's late. Or very early. Either way, I'm here.",
    "The best ideas come when everyone else is asleep.",
    "A coffee at witching hour? Bold. I admire that.",
  ],
  morning: [
    "The sun is up, and so are the ideas.",
    "The coffee is calling – but first, me.",
    "Early risers ask the best questions.",
    "Today's going to be a good one. I can feel it.",
    "New day, new luck, new tabs.",
    "Before the world wakes, we're already at work.",
    "The early bird catches the worm – we catch the bug.",
    "Morning hours are worth their weight in gold.",
    "Breathed today? Perfect, then let's begin.",
    "One more sip of coffee, then we're off.",
    "Back to zero, anything is possible.",
    "A fresh day, a blank page, a ready agent.",
  ],
  day: [
    "Midday high – time for good ideas.",
    "What'll it be today?",
    "Between two meetings? Perfect.",
    "The sun is high, and so are the questions.",
    "Coffee number three? I'm not counting.",
    "I'm ready when you are.",
    "Make yourself comfortable, the day is yours.",
    "A day like a painting – only your question is missing.",
    "Afternoon slump? A good prompt helps.",
    "Learned anything wild today?",
    "All in the green. Mostly.",
    "Busy as a beaver, just with less wood.",
  ],
  evening: [
    "The evening belongs to the curious.",
    "The stars are on, so are we.",
    "Time for tea and good thoughts.",
    "Golden hour – ideal for big plans.",
    "The day was long, the questions are longer.",
    "Get cozy, let's wind down the day.",
    "An evening like a good novel.",
    "The couch is calling. So am I.",
    "Dusk: the hour of thinkers.",
    "Let's end the day with something good.",
    "Netflix can wait, we've got more important things.",
    "Calling it a day? Not when a good idea is on its way.",
  ],
};

const WEEKDAY_QUIPS = {
  1: ["Monday. We'll get through it. Somehow.", "Monday already lost the moment you showed up.", "Monday mode: on. Motivation: loading …"],
  2: ["Tuesday – the unsung hero of the week.", "Tuesday: neither start nor end, just a good time for good things."],
  3: ["Hump day! It's all downhill from here – in a good way.", "Wednesday: the week is halfway through, and you've got the wind at your back."],
  4: ["Thursday: almost Friday, but not quite.", "One more day until the weekend. Almost excited already."],
  5: ["Friday! The weekend is waving from afar.", "Almost there – just a few questions until the weekend."],
  6: ["Saturday: a day for little adventures.", "Weekend! Working today is your own fault – but I'm happy to help."],
  0: ["Sunday – time for the things you enjoy.", "Sunday peace, with a chat on the side."],
};

const SPECIAL = {
  christmas: {
    titles: ["Merry Christmas!", "Ho ho ho!", "Happy Christmas!", "Hohoho!", "Season's greetings!", "It's beginning to look a lot like Christmas"],
    quips: [
      "Year after year – now with a prompt.",
      "Last Christmas … I'll give you my full attention.",
      "Snowflakes, sugar cookies – how can I help?",
      "Silent night, holy night – and a chat that never sleeps.",
      "Santa brought you the best agent.",
      "Cookies for the server, gingerbread for you.",
      "Oh, what fun it is to prompt and sing.",
      "Rudolph has the day off today. I don't.",
      "Jingle bells, jingle bells, prompt all the way.",
      "Tinsel for everyone!",
      "Cracking nuts, cracking questions – count me in.",
      "Under the tree, nothing beats a good question.",
    ],
  },
  newYearsEve: {
    titles: ["Happy New Year's Eve!", "Cheers to the new year!", "New Year's Eve!", "Just a few hours to go …"],
    quips: [
      "Dinner for One? Same procedure as every year.",
      "New year's resolutions? I'll remember them; you'll forget them anyway.",
      "Lead pouring is out – asking questions is in.",
      "The fireworks are waiting. So are the questions.",
      "Fireworks outside, please. Questions right here.",
      "A glass of champagne to the old year.",
      "The old year was good. The next one will be better.",
      "Berliners? I only have bytes.",
    ],
  },
  newYear: {
    titles: ["Happy New Year!", "Cheers to the new year!", "New year, new luck!"],
    quips: [
      "The year is still fresh – like your resolutions.",
      "Anything is still possible. Just start.",
      "New year, new bugs, old love.",
      "The hangover passes. The ideas stay.",
      "365 days full of chances – number one: this chat.",
      "The best is yet to come.",
    ],
  },
  easter: {
    titles: ["Happy Easter!", "Hop, hop!", "Happy Easter holiday!", "Hello, Easter bunny!"],
    quips: [
      "I've hidden a few good answers.",
      "Egg hunting or asking questions – your choice.",
      "The Easter bunny has nothing against prompts.",
      "Colorful as an Easter egg: this chat.",
      "Bunny strong. Literally.",
      "Eggs outside, ideas in here.",
      "Spring is in the air again …",
      "I'll leave the egg hunt to you; I'll take the questions.",
    ],
  },
  birthday: {
    titles: ["Happy Birthday!", "Many happy returns!", "Congratulations!", "Today's your day!"],
    quips: [
      "You're the center of the universe today.",
      "Blow out the candles, make a wish – I'll help it come true.",
      "A year older, a year wiser, two years cooler.",
      "Cake for you, bits and bytes for me.",
      "Today you can ask something truly wild.",
      "Your chat is celebrating too – minus the confetti.",
      "Today it's all yours: the cake, the glory, the agent.",
      "Make a wish – or just ask me.",
    ],
  },
};

// [headline, optional second line]. Picked in 5 % of all cases, on purpose silly.
const MEMES = [
  ["trololololol", "ohohohohoh …"],
  ["Never gonna give you up", "Never gonna let you down."],
  ["Hello there.", "General Kenobi!"],
  ["Bruh.", ""],
  ["It's Wednesday, my dudes.", ""],
  ["A wild prompt appears!", "What will you do?"],
  ["Stonks ↗", "Your agent is bullish."],
  ["I have no idea what I'm doing,", "but I have a lot of confidence."],
  ["Error 404: Motivation not found.", "But here's a chat."],
  ["It wasn't me,", "I know nothing."],
  ["AAAAAAAAAAAAAAAAAA", "(Sorry. Had to get that out.)"],
  ["Skibidi Ceres Rizz", ""],
  ["Sigma grindset:", "Enter prompt."],
  ["*Windows startup sound*", ""],
  ["Sheeeeesh.", ""],
  ["Nice.", "Nice."],
  ["E.", ""],
  ["It's not a bug,", "it's a feature."],
  ["My brain has 1000 tabs open,", "999 of them are playing music."],
  ["I'm a gamer,", "I don't need sleep."],
  ["Bonk.", ""],
  ["Achievement unlocked:", "You opened a chat."],
  ["Lol.", "Lmao."],
  ["This is a chat.", "This is the truth."],
  ["Ctrl + Alt + Delete", "and then just carry on."],
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
