//! Generator of the benchmark set (spec §12.4): 1000 synthetic notes, 40% Russian, 30%
//! English, 20% code and 10% mixed, each with a text of 100–8192 UTF-16 units drawn uniformly,
//! from a fixed seed. The notes named by `queries.json` carry its planted paragraphs: the
//! texts the queries look for. Everything else is filler built from sentence templates of
//! everyday topics and code snippets, which never contain the planted words.
//!
//! The module has no dependencies besides `serde`: `tests/bench.rs` and
//! `examples/bench_corpus.rs` of the worker include it with `#[path]`.

use std::fmt::Write as _;

use serde::Deserialize;

/// Seed of the generator.
pub const SEED: u64 = 0x5EA2_C4B3_17A0_2026;
/// Number of notes.
pub const NOTES: usize = 1000;
/// Shortest text of a note, in UTF-16 units.
pub const MIN_UTF16: usize = 100;
/// Longest text of a note, in UTF-16 units.
pub const MAX_UTF16: usize = 8192;

/// Language of a note; note `i` has the kind of `i % 10`: 0–3 Russian, 4–6 English, 7–8 code,
/// 9 mixed.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Kind {
    Ru,
    En,
    Code,
    Mixed,
}

impl Kind {
    /// Kind of note `i`.
    #[must_use]
    pub fn of(i: usize) -> Kind {
        match i % 10 {
            0..=3 => Kind::Ru,
            4..=6 => Kind::En,
            7 | 8 => Kind::Code,
            _ => Kind::Mixed,
        }
    }
}

/// A note of the set.
#[derive(Clone, Debug)]
pub struct Note {
    /// `n0000`…`n0999`.
    pub id: String,
    pub kind: Kind,
    pub name: String,
    pub info: String,
}

/// The queries file: planted paragraphs and queries.
#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Set {
    pub plants: Vec<Plant>,
    pub queries: Vec<BenchQuery>,
}

/// A paragraph planted into note `note`; `title` replaces the note name.
#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Plant {
    pub note: usize,
    pub kind: Kind,
    pub title: String,
    pub lines: Vec<String>,
}

/// A query with the note it must find, or `None` for a query that must find nothing.
#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct BenchQuery {
    pub q: String,
    /// `word`, `phrase`, `substring`, `code`, `operator`, `semantic`, `cross`, `nonsense`
    /// or `absent`.
    pub kind: String,
    pub expect: Option<usize>,
}

/// Parses the queries file.
///
/// # Panics
///
/// On malformed JSON, or when a plant does not match the kind of its note.
#[must_use]
pub fn parse_set(json: &str) -> Set {
    let set: Set = serde_json::from_str(json).expect("queries.json");
    for p in &set.plants {
        assert!(p.note < NOTES, "plant note {}", p.note);
        assert_eq!(Kind::of(p.note), p.kind, "kind of plant note {}", p.note);
    }
    set
}

/// The `SplitMix64` generator.
pub struct Rng(u64);

impl Rng {
    #[must_use]
    pub fn new(seed: u64) -> Self {
        Self(seed)
    }

    pub fn next_u64(&mut self) -> u64 {
        self.0 = self.0.wrapping_add(0x9E37_79B9_7F4A_7C15);
        let mut z = self.0;
        z = (z ^ (z >> 30)).wrapping_mul(0xBF58_476D_1CE4_E5B9);
        z = (z ^ (z >> 27)).wrapping_mul(0x94D0_49BB_1331_11EB);
        z ^ (z >> 31)
    }

    /// Uniform in `lo..=hi`.
    pub fn range(&mut self, lo: usize, hi: usize) -> usize {
        let span = u64::try_from(hi - lo + 1).expect("span");
        lo + usize::try_from(self.next_u64() % span).expect("below span")
    }

    pub fn pick<'a>(&mut self, items: &[&'a str]) -> &'a str {
        items[self.range(0, items.len() - 1)]
    }

    /// `true` with probability `percent`/100.
    pub fn chance(&mut self, percent: usize) -> bool {
        self.range(0, 99) < percent
    }
}

/// The notes of the set with the plants of `set`.
///
/// # Panics
///
/// When two plants name one note.
#[must_use]
pub fn corpus(set: &Set) -> Vec<Note> {
    let mut rng = Rng::new(SEED);
    (0..NOTES)
        .map(|i| {
            let plant = {
                let mut found = set.plants.iter().filter(|p| p.note == i);
                let plant = found.next();
                assert!(found.next().is_none(), "two plants for note {i}");
                plant
            };
            note(&mut rng, i, plant)
        })
        .collect()
}

fn note(rng: &mut Rng, i: usize, plant: Option<&Plant>) -> Note {
    let kind = Kind::of(i);
    let target = rng.range(MIN_UTF16, MAX_UTF16);
    let topic = rng.range(0, TOPICS.len() - 1);
    let filler_name = name(rng, kind, topic);
    let planted = plant.map(|p| p.lines.join("\n"));
    let planted_len = planted.as_deref().map_or(0, utf16_len);
    // The plant counts towards the length; a plant longer than the target is the whole text.
    let budget = target.saturating_sub(planted_len + 2);
    let mut paragraphs = Vec::new();
    let mut len = 0;
    while len < budget {
        let p = paragraph(rng, kind, topic);
        len += utf16_len(&p) + 2;
        paragraphs.push(p);
    }
    let mut info = paragraphs.join("\n\n");
    truncate_utf16(&mut info, budget);
    let info = info.trim_end().to_owned();
    let info = match planted {
        None => info,
        Some(planted) => {
            let mut parts: Vec<&str> = if info.is_empty() {
                Vec::new()
            } else {
                info.split("\n\n").collect()
            };
            let at = rng.range(0, parts.len());
            parts.insert(at, &planted);
            parts.join("\n\n")
        }
    };
    Note {
        id: format!("n{i:04}"),
        kind,
        name: plant.map_or(filler_name, |p| p.title.clone()),
        info,
    }
}

fn utf16_len(s: &str) -> usize {
    s.encode_utf16().count()
}

/// Cuts `s` to at most `units` UTF-16 units, on a character boundary.
fn truncate_utf16(s: &mut String, units: usize) {
    let mut seen = 0;
    for (byte, c) in s.char_indices() {
        seen += c.len_utf16();
        if seen > units {
            s.truncate(byte);
            return;
        }
    }
}

fn name(rng: &mut Rng, kind: Kind, topic: usize) -> String {
    let t = &TOPICS[topic];
    let template = match kind {
        Kind::Ru => rng.pick(t.ru_titles).to_owned(),
        Kind::En => rng.pick(t.en_titles).to_owned(),
        Kind::Code => rng.pick(CODE_TITLES).to_owned(),
        Kind::Mixed => {
            let ru = rng.pick(t.ru_titles);
            let en = rng.pick(t.en_titles);
            format!("{ru} / {en}")
        }
    };
    fill(rng, &template)
}

/// A paragraph: sentences of the note's topic (or, now and then, of another topic) on one line
/// or as a list, or a code snippet.
fn paragraph(rng: &mut Rng, kind: Kind, topic: usize) -> String {
    let topic = if rng.chance(30) {
        rng.range(0, TOPICS.len() - 1)
    } else {
        topic
    };
    let t = &TOPICS[topic];
    match kind {
        Kind::Ru => prose(rng, t.ru),
        Kind::En => prose(rng, t.en),
        Kind::Code => {
            if rng.chance(25) {
                let comment = if rng.chance(50) { t.en } else { t.ru };
                let template = rng.pick(comment);
                format!("// {}", fill(rng, template))
            } else {
                snippet(rng)
            }
        }
        Kind::Mixed => match rng.range(0, 4) {
            0 | 1 => prose(rng, t.ru),
            2 | 3 => prose(rng, t.en),
            _ => snippet(rng),
        },
    }
}

fn snippet(rng: &mut Rng) -> String {
    let template = rng.pick(SNIPPETS);
    fill(rng, template)
}

fn prose(rng: &mut Rng, sentences: &[&str]) -> String {
    let count = rng.range(2, 5);
    let list = rng.chance(25);
    let mut used: Vec<&str> = Vec::with_capacity(count);
    let mut out = String::new();
    for i in 0..count {
        // A sentence once a paragraph.
        let mut template = rng.pick(sentences);
        while used.contains(&template) {
            template = rng.pick(sentences);
        }
        used.push(template);
        let s = fill(rng, template);
        if list {
            if i > 0 {
                out.push('\n');
            }
            out.push_str("- ");
        } else if i > 0 {
            out.push(' ');
        }
        out.push_str(&s);
    }
    out
}

/// Replaces the slots of a template: `{n}` a number, `{t}` a time, `{ru_name}`, `{en_name}`,
/// `{ru_day}`, `{en_day}`, `{city}`, `{e}`/`{E}` an entity of code in lower/upper camel case,
/// `{v}` a verb of code.
fn fill(rng: &mut Rng, template: &str) -> String {
    let mut out = String::with_capacity(template.len() + 16);
    let mut rest = template;
    while let Some(open) = rest.find('{') {
        out.push_str(&rest[..open]);
        let Some(close) = rest[open..].find('}') else {
            break;
        };
        let slot = &rest[open + 1..open + close];
        match slot {
            "n" => {
                let _ = write!(out, "{}", rng.range(2, 95));
            }
            "t" => {
                let _ = write!(out, "{}:{:02}", rng.range(8, 21), rng.range(0, 3) * 15);
            }
            "ru_name" => out.push_str(rng.pick(RU_NAMES)),
            "en_name" => out.push_str(rng.pick(EN_NAMES)),
            "ru_day" => out.push_str(rng.pick(RU_DAYS)),
            "en_day" => out.push_str(rng.pick(EN_DAYS)),
            "city" => out.push_str(rng.pick(CITIES)),
            "e" => out.push_str(rng.pick(ENTITIES)),
            "E" => {
                let e = rng.pick(ENTITIES);
                let mut chars = e.chars();
                if let Some(first) = chars.next() {
                    out.extend(first.to_uppercase());
                    out.push_str(chars.as_str());
                }
            }
            "v" => out.push_str(rng.pick(VERBS)),
            // Not a slot: `{` of code.
            _ => {
                out.push('{');
                rest = &rest[open + 1..];
                continue;
            }
        }
        rest = &rest[open + close + 1..];
    }
    out.push_str(rest);
    out
}

const RU_NAMES: &[&str] = &[
    "Андрей",
    "Мария",
    "Ирина",
    "Дмитрий",
    "Сергей",
    "Наталья",
    "Павел",
    "Елена",
    "Артём",
    "Ольга",
];
const EN_NAMES: &[&str] = &[
    "Alice", "Brian", "Chloe", "Daniel", "Emma", "Frank", "Grace", "Henry", "Isabel", "Jack",
];
const RU_DAYS: &[&str] = &[
    "понедельник",
    "вторник",
    "среду",
    "четверг",
    "пятницу",
    "субботу",
    "воскресенье",
];
const EN_DAYS: &[&str] = &[
    "Monday",
    "Tuesday",
    "Wednesday",
    "Thursday",
    "Friday",
    "Saturday",
    "Sunday",
];
const CITIES: &[&str] = &[
    "Казань",
    "Самара",
    "Ярославль",
    "Пермь",
    "Тверь",
    "Вологда",
    "Псков",
];
const ENTITIES: &[&str] = &[
    "invoice", "profile", "session", "report", "ticket", "comment", "upload", "metric", "payment",
    "widget",
];
const VERBS: &[&str] = &[
    "load", "save", "update", "validate", "render", "fetch", "encode", "sync",
];

/// An everyday topic: titles and sentence templates in both languages.
struct Topic {
    ru_titles: &'static [&'static str],
    en_titles: &'static [&'static str],
    ru: &'static [&'static str],
    en: &'static [&'static str],
}

const TOPICS: &[Topic] = &[
    // Work.
    Topic {
        ru_titles: &[
            "Планёрка {ru_day}",
            "Задачи на неделю",
            "Отчёт для {ru_name}",
        ],
        en_titles: &[
            "Weekly sync",
            "Tasks for the sprint",
            "Notes from the {en_day} meeting",
        ],
        ru: &[
            "На планёрке {ru_name} рассказала о сроках по отчёту.",
            "Презентацию для руководства нужно закончить к {ru_day}.",
            "Созвон с подрядчиком перенесли на {t}.",
            "{ru_name} возьмёт на себя сверку таблиц за прошлый месяц.",
            "Обсудили, какие задачи можно отложить до следующего спринта.",
            "В пятницу демонстрация результатов, нужно подготовить {n} слайдов.",
            "Договорились отвечать на письма клиентов в течение одного рабочего дня.",
            "Протокол встречи разослать всем участникам до вечера.",
        ],
        en: &[
            "{en_name} will send the updated timeline before {en_day}.",
            "The status report is due at {t}, keep it short.",
            "We agreed to move the design review to next week.",
            "Action item: {en_name} collects feedback from the support team.",
            "The budget spreadsheet needs {n} more rows for contractors.",
            "Demo on {en_day}: prepare screenshots and a short summary.",
            "Nobody owns the release notes yet, raise it at standup.",
            "Meeting notes go to the shared folder after every call.",
        ],
    },
    // Cooking.
    Topic {
        ru_titles: &["Что приготовить", "Рецепты на выходные", "Меню на {ru_day}"],
        en_titles: &["Dinner ideas", "Weekend recipes", "Menu for {en_day}"],
        ru: &[
            "Макароны отварить в подсоленной воде {n} минут.",
            "Для омлета взбить три яйца с молоком и щепоткой соли.",
            "Салат из огурцов заправить сметаной и укропом.",
            "Курицу запекать при 180 градусах около часа.",
            "Гречку промыть и варить под крышкой на слабом огне.",
            "Сырники обжарить на сковороде с двух сторон до румяной корочки.",
            "Купить сливочное масло, сыр и упаковку риса.",
            "Суп из чечевицы получается густым, если добавить картофель.",
        ],
        en: &[
            "Boil the pasta for {n} minutes in salted water.",
            "Roast the chicken at 180 degrees for about an hour.",
            "Whisk three eggs with a splash of milk for the omelette.",
            "The lentil soup gets thicker with one more potato.",
            "Buy butter, cheese and a bag of rice on the way home.",
            "Pancakes need a hot pan and a little oil.",
            "Chop the cucumbers and dress the salad with yogurt.",
            "Leftover rice works well for fried rice the next day.",
        ],
    },
    // Travel.
    Topic {
        ru_titles: &[
            "Поездка в {city}",
            "Путешествие летом",
            "Билеты и гостиница",
        ],
        en_titles: &[
            "Trip to the coast",
            "Summer travel plan",
            "Tickets and hotel",
        ],
        ru: &[
            "Билеты на поезд до города {city} купить заранее.",
            "Гостиница в центре, заезд после {t}.",
            "В музее по {ru_day} бесплатный вход до полудня.",
            "Взять с собой зарядку, паспорт и зонт.",
            "Экскурсия по старому городу длится {n} минут.",
            "Багаж сдать в камеру хранения на вокзале.",
            "{ru_name} советует попробовать местную кухню в кафе у набережной.",
            "Обратный поезд отправляется в {t}.",
        ],
        en: &[
            "Train tickets are cheaper if booked a month ahead.",
            "Hotel check-in starts at {t}, breakfast is included.",
            "The museum is free on {en_day} mornings.",
            "Pack the charger, passport and an umbrella.",
            "The old town walking tour takes {n} minutes.",
            "Leave the luggage at the station before the tour.",
            "{en_name} recommends the cafe near the harbour.",
            "The return train leaves at {t}.",
        ],
    },
    // Home.
    Topic {
        ru_titles: &["Дела по дому", "Уборка", "Квартира"],
        en_titles: &["House chores", "Cleaning plan", "Apartment"],
        ru: &[
            "Поменять лампочку в коридоре и купить запасные.",
            "Вызвать сантехника: на кухне подтекает кран.",
            "Генеральную уборку сделать в {ru_day}.",
            "Постирать шторы и пледы на деликатном режиме.",
            "Повесить полку в ванной, нужны дюбели на {n} мм.",
            "Счётчики воды передать до двадцатого числа.",
            "{ru_name} обещал помочь собрать шкаф.",
            "Разобрать антресоли и отдать ненужные вещи.",
        ],
        en: &[
            "Replace the hallway light bulb and buy spares.",
            "Call the plumber about the dripping kitchen tap.",
            "Deep cleaning on {en_day}, start with the windows.",
            "Wash the curtains on the delicate cycle.",
            "Hang the bathroom shelf, it needs {n} mm anchors.",
            "Submit the water meter readings before the twentieth.",
            "{en_name} offered to help assemble the wardrobe.",
            "Sort out the storage shelf and donate old clothes.",
        ],
    },
    // Sport.
    Topic {
        ru_titles: &["Тренировки", "Бег", "План занятий"],
        en_titles: &["Workouts", "Running log", "Training plan"],
        ru: &[
            "Пробежка {n} минут в спокойном темпе.",
            "В бассейне проплыл двадцать бассейнов кролем.",
            "Йога по {ru_day} в {t}, взять коврик.",
            "Растяжка после тренировки обязательна.",
            "Пульс на интервалах держать ниже ста семидесяти.",
            "В зале добавить жим лёжа и тягу.",
            "Новые кроссовки удобнее на длинных дистанциях.",
            "{ru_name} зовёт на забег в парке в субботу.",
        ],
        en: &[
            "Easy run for {n} minutes, keep the pace low.",
            "Swam twenty laps of freestyle at the pool.",
            "Yoga class on {en_day} at {t}, bring a mat.",
            "Stretch for ten minutes after every workout.",
            "Keep the heart rate under 170 on intervals.",
            "Add bench press and rows to the gym day.",
            "The new running shoes feel better on long runs.",
            "{en_name} signed us up for the park race.",
        ],
    },
    // Money.
    Topic {
        ru_titles: &["Бюджет", "Расходы за месяц", "Платежи"],
        en_titles: &["Budget", "Monthly expenses", "Bills to pay"],
        ru: &[
            "Оплатить интернет и коммунальные услуги до {n} числа.",
            "На продукты в этом месяце ушло больше, чем планировали.",
            "Отложить десять процентов зарплаты на накопительный счёт.",
            "Подписку на музыку можно отменить.",
            "{ru_name} вернёт долг в {ru_day}.",
            "Сравнить тарифы мобильной связи.",
            "Записать все крупные покупки в таблицу расходов.",
            "Кешбэк по карте приходит в начале месяца.",
        ],
        en: &[
            "Pay the internet and utility bills before the {n}th.",
            "Groceries cost more this month than planned.",
            "Move ten percent of the salary to savings.",
            "The music subscription can be cancelled.",
            "{en_name} pays back the loan on {en_day}.",
            "Compare mobile plans before the contract ends.",
            "Log every large purchase in the expenses sheet.",
            "Card cashback arrives at the start of the month.",
        ],
    },
    // Learning.
    Topic {
        ru_titles: &["Что почитать", "Курсы", "Конспект лекции"],
        en_titles: &["Reading list", "Online courses", "Lecture notes"],
        ru: &[
            "Дочитать роман до конца месяца, осталось {n} страниц.",
            "Онлайн-курс по статистике: третья неделя, домашнее задание.",
            "Подкаст про историю науки слушать по дороге на работу.",
            "Выписать цитаты из книги в отдельную заметку.",
            "Лекция начинается в {t}, ссылку пришлёт {ru_name}.",
            "Повторить конспект перед экзаменом.",
            "В библиотеке можно взять учебник на две недели.",
            "Записаться на вебинар по публичным выступлениям.",
        ],
        en: &[
            "Finish the novel this month, {n} pages left.",
            "Statistics course, week three: submit the homework.",
            "Listen to the history of science podcast on the commute.",
            "Copy the best quotes from the book into a separate note.",
            "The lecture starts at {t}, {en_name} shares the link.",
            "Review the notes before the exam.",
            "The library lends textbooks for two weeks.",
            "Sign up for the public speaking webinar.",
        ],
    },
    // Garden.
    Topic {
        ru_titles: &["Сад", "Цветы на балконе", "Огород"],
        en_titles: &["Garden", "Balcony flowers", "Backyard"],
        ru: &[
            "Подстричь газон и убрать листья.",
            "Яблони побелить весной.",
            "Огурцы посадить вдоль забора.",
            "Цветы на балконе поливать вечером.",
            "Купить удобрение для роз.",
            "{ru_name} поделится луковицами тюльпанов.",
            "Обрезать сухие ветки у смородины.",
            "Компост перемешать через {n} дней.",
        ],
        en: &[
            "Mow the lawn and rake the leaves.",
            "Prune the dead branches of the currant bush.",
            "Plant cucumbers along the fence.",
            "Water the balcony flowers in the evening.",
            "Buy fertiliser for the roses.",
            "{en_name} will share some tulip bulbs.",
            "Turn the compost in {n} days.",
            "The apple trees need whitewash in spring.",
        ],
    },
    // Pets.
    Topic {
        ru_titles: &["Собака", "Питомцы", "Аквариум"],
        en_titles: &["Dog", "Pets", "Fish tank"],
        ru: &[
            "Гулять с собакой утром и вечером по {n} минут.",
            "Купить новый поводок и игрушку.",
            "В аквариуме поменять треть воды.",
            "Наполнитель для лотка заканчивается.",
            "{ru_name} присмотрит за собакой в выходные.",
            "Подстричь когти и вычесать шерсть.",
            "Собака боится грозы, оставить включённым радио.",
            "Рыбок кормить один раз в день.",
        ],
        en: &[
            "Walk the dog for {n} minutes morning and evening.",
            "Buy a new leash and a chew toy.",
            "Change a third of the water in the fish tank.",
            "The litter box needs a refill.",
            "{en_name} will dog-sit over the weekend.",
            "Trim the claws and brush the fur.",
            "The dog is afraid of thunder, leave the radio on.",
            "Give the fish a pinch once a day.",
        ],
    },
    // IT operations, in prose.
    Topic {
        ru_titles: &["Сервер", "Резервные копии", "Мониторинг"],
        en_titles: &["Server", "Backups", "Monitoring"],
        ru: &[
            "Проверить место на диске сервера, свободно {n} процентов.",
            "Резервную копию базы делать каждую ночь в {t}.",
            "Сертификат сайта истекает через месяц, продлить заранее.",
            "Настроить дашборд с задержками ответов.",
            "{ru_name} обновит операционную систему на тестовом стенде.",
            "Пароли хранить только в менеджере паролей.",
            "После обновления перезапустить службу и проверить статус.",
            "Восстановление из копии проверять раз в квартал.",
        ],
        en: &[
            "Check the disk space on the server, {n} percent free.",
            "Nightly database backup runs at {t}.",
            "The site certificate expires next month, renew early.",
            "Set up a dashboard for response latency.",
            "{en_name} upgrades the operating system on staging.",
            "Keep passwords only in the password manager.",
            "Restart the service after the upgrade and check its status.",
            "Test a restore from backup once a quarter.",
        ],
    },
    // Family.
    Topic {
        ru_titles: &["День рождения", "Семья", "Подарки"],
        en_titles: &["Birthday", "Family", "Gift ideas"],
        ru: &[
            "У {ru_name} день рождения в {ru_day}, заказать торт.",
            "Позвонить бабушке и спросить про лекарства.",
            "Родительское собрание в школе в {t}.",
            "Идеи подарков: книга, настольная игра, шарф.",
            "Забрать детей из секции в шесть.",
            "Фотографии с праздника отправить родственникам.",
            "На выходных поехать к родителям.",
            "Купить открытку и цветы.",
        ],
        en: &[
            "{en_name}'s birthday is on {en_day}, order the cake.",
            "Call grandma and ask about her medicine.",
            "Parent-teacher meeting at school at {t}.",
            "Gift ideas: a book, a board game, a scarf.",
            "Pick up the kids from practice at six.",
            "Send the party photos to the relatives.",
            "Visit the parents over the weekend.",
            "Buy a card and some flowers.",
        ],
    },
    // Hobbies.
    Topic {
        ru_titles: &["Фотография", "Настольные игры", "Кино"],
        en_titles: &["Photography", "Board games", "Movies"],
        ru: &[
            "Снимать на закате с выдержкой подольше.",
            "Настольные игры у {ru_name} в {ru_day}.",
            "Посмотреть новый фильм в кинотеатре.",
            "Распечатать лучшие фотографии в формате {n} на 15.",
            "Концерт начинается в {t}, билеты в телефоне.",
            "Протереть объектив и зарядить аккумулятор камеры.",
            "Составить список сериалов на зиму.",
            "Собрать пазл на тысячу деталей.",
        ],
        en: &[
            "Shoot at sunset with a longer exposure.",
            "Board game night at {en_name}'s place on {en_day}.",
            "Watch the new film at the cinema.",
            "Print the best photos at {n} by 15.",
            "The concert starts at {t}, tickets are on the phone.",
            "Clean the lens and charge the camera battery.",
            "Make a list of series to watch this winter.",
            "Finish the thousand-piece puzzle.",
        ],
    },
];

const CODE_TITLES: &[&str] = &[
    "{E} service",
    "Snippets: {e}",
    "handler for {e}",
    "{v} {e} notes",
    "SQL for {e}",
    "scripts",
];

const SNIPPETS: &[&str] = &[
    "func {v}{E}(ctx context.Context, id string) (*{E}, error) {
\trow := db.QueryRowContext(ctx, \"SELECT data FROM {e}s WHERE id = $1\", id)
\tvar out {E}
\tif err := row.Scan(&out.Data); err != nil {
\t\treturn nil, fmt.Errorf(\"{v} {e}: %w\", err)
\t}
\treturn &out, nil
}",
    "type {E}Store struct {
\tmu    sync.Mutex
\titems map[string]*{E}
}

func (s *{E}Store) Get(id string) (*{E}, bool) {
\ts.mu.Lock()
\tdefer s.mu.Unlock()
\tv, ok := s.items[id]
\treturn v, ok
}",
    "for i, item := range items {
\tif item.Size > limit {
\t\tlog.Printf(\"skip %d: too large\", i)
\t\tcontinue
\t}
\ttotal += item.Size
}",
    "pub fn {v}_{e}(input: &str) -> Result<{E}, Error> {
    let fields: Vec<&str> = input.split(',').map(str::trim).collect();
    if fields.len() != {n} {
        return Err(Error::Fields(fields.len()));
    }
    Ok({E}::from_fields(&fields)?)
}",
    "#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct {E} {
    pub id: u64,
    pub title: String,
    pub tags: Vec<String>,
}",
    "match state {
    State::Idle => start(),
    State::Running(job) if job.done() => finish(job),
    State::Running(_) => {}
}",
    "def {v}_{e}(path):
    with open(path, encoding=\"utf-8\") as f:
        rows = [line.strip().split(\";\") for line in f if line.strip()]
    return [{E}(*row) for row in rows]",
    "class {E}Cache:
    def __init__(self, size={n}):
        self.size = size
        self.items = {}

    def get(self, key):
        return self.items.get(key)",
    "items = sorted(items, key=lambda x: (x.priority, x.created))
print(f\"{len(items)} items, first: {items[0].title}\")",
    "export async function {v}{E}(id) {
  const res = await fetch(`/api/{e}s/${id}`);
  if (!res.ok) throw new Error(`{v} {e}: ${res.status}`);
  return res.json();
}",
    "const total = list.filter((x) => x.active).map((x) => x.amount).reduce((a, b) => a + b, 0);",
    "document.querySelector('#{e}-form').addEventListener('submit', (event) => {
  event.preventDefault();
  {v}{E}(new FormData(event.target));
});",
    "SELECT id, title, created_at
FROM {e}s
WHERE created_at > now() - interval '{n} days'
ORDER BY created_at DESC
LIMIT 50;",
    "CREATE INDEX {e}s_created_idx ON {e}s (created_at);
UPDATE {e}s SET archived = true WHERE created_at < '2024-01-01';",
    "#!/bin/sh
set -eu
for f in ./data/*.csv; do
  echo \"processing $f\"
  ./bin/import --table {e}s \"$f\"
done",
    "git checkout -b fix/{e}-{v}
go test ./... -run Test{E} -count=1
git commit -am \"{e}: fix {v}\"",
    "docker compose up -d db
docker compose logs -f api | grep -i error",
    "server:
  port: 80{n}
  workers: 4
storage:
  path: /var/lib/app/{e}s",
    "{\"name\": \"{e}-service\", \"version\": \"1.{n}.0\", \"private\": true}",
];
