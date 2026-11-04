//! Search form and query parsing on arbitrary text: no panic, the form equals the whole-line
//! normalization, the offset map is monotonic and exact at both ends.
#![no_main]

use libfuzzer_sys::fuzz_target;
use unicode_normalization::UnicodeNormalization;
use worker::text::{parse_query, search_form};

fuzz_target!(|line: &str| {
    let form = search_form(line);
    let whole = line
        .nfc()
        .map(|c| match c {
            'ё' => 'е',
            'Ё' => 'Е',
            c => c,
        })
        .collect::<String>()
        .to_lowercase();
    assert_eq!(form.text, whole);

    let len = u32::try_from(line.encode_utf16().count()).expect("short input");
    assert_eq!(form.utf16_at(0), 0);
    assert_eq!(form.utf16_at(form.text.len()), len);
    let mut prev = 0;
    for b in 0..=form.text.len() {
        let (start, end) = (form.utf16_at(b), form.utf16_end_at(b));
        assert!(prev <= start && start <= end && end <= len);
        prev = start;
    }

    let _ = parse_query(line);
});
