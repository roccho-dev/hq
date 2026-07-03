use std::env;
use std::process;

#[derive(Clone, Debug)]
struct Property {
    key: &'static str,
    detail: &'static str,
    required: bool,
    values: &'static [&'static str],
}

static PROPERTIES: &[Property] = &[
    Property {
        key: "status",
        detail: "current work state",
        required: true,
        values: &["done", "draft", "deferred"],
    },
    Property {
        key: "title",
        detail: "short task title",
        required: true,
        values: &[],
    },
    Property {
        key: "assignee",
        detail: "owner or reviewer",
        required: false,
        values: &[],
    },
    Property {
        key: "priority",
        detail: "triage priority",
        required: false,
        values: &["low", "normal", "high"],
    },
    Property {
        key: "due",
        detail: "target date",
        required: false,
        values: &[],
    },
];

#[derive(Clone, Debug, PartialEq, Eq)]
enum CursorState {
    ObjectOpen,
    Key,
    Value,
    NextKey,
    Other,
}

#[derive(Clone, Debug)]
struct CursorContext {
    state: CursorState,
    partial: String,
    present_keys: Vec<String>,
    current_key: Option<String>,
}

#[derive(Clone, Debug)]
struct Suggestion {
    label: String,
    kind: &'static str,
    detail: String,
    score: i32,
    replace_partial: String,
    insert_text: String,
    compile_op: &'static str,
    key: Option<String>,
    value: Option<String>,
}

fn main() {
    let args: Vec<String> = env::args().skip(1).collect();
    let command = args.first().map(String::as_str).unwrap_or("demo-autocomplete");

    match command {
        "demo-autocomplete" => print!("{}", demo_autocomplete()),
        "suggest" => {
            let buffer = arg_value(&args, "--buffer").unwrap_or_else(|| "{".to_string());
            let query = arg_value(&args, "--query");
            for item in suggest(&buffer, query.as_deref()) {
                println!("{}", candidate_json(&item));
            }
        }
        "accept" => {
            let buffer = arg_value(&args, "--buffer").unwrap_or_else(|| "{".to_string());
            let query = arg_value(&args, "--query");
            let index = arg_value(&args, "--index")
                .and_then(|raw| raw.parse::<usize>().ok())
                .unwrap_or(0);
            let candidates = suggest(&buffer, query.as_deref());
            if let Some(item) = candidates.get(index) {
                println!("{}", queue_create_json(item));
            } else {
                eprintln!("no candidate at index {index}");
                process::exit(1);
            }
        }
        "version" | "--version" | "-V" => println!("hq 0.0.0 rust-suggest-proof"),
        "help" | "--help" | "-h" => print_help(),
        other => {
            eprintln!("unknown command: {other}");
            print_help();
            process::exit(2);
        }
    }
}

fn print_help() {
    println!("usage: hq [demo-autocomplete|suggest|accept|version]");
    println!("       hq suggest --buffer <json-fragment> [--query <text>]");
    println!("       hq accept  --buffer <json-fragment> [--query <text>] [--index <n>]");
}

fn arg_value(args: &[String], flag: &str) -> Option<String> {
    args.windows(2)
        .find(|pair| pair[0] == flag)
        .map(|pair| pair[1].clone())
}

fn demo_autocomplete() -> String {
    let cases = [
        ("key fuzzy: ai -> assignee", "{\"a", Some("ai")),
        ("value fuzzy: d -> done/draft/deferred", "{\"status\": \"d", None),
        ("field fuzzy: pri -> priority", "{\"status\": \"done\", \"pri", None),
        ("queue create: accept top suggestion", "{", None),
    ];

    let mut out = String::new();
    out.push_str("hq rust suggestion proof\n\n");
    out.push_str("surface\n  matcher-ranked cmp-like suggestions\n\n");
    out.push_str("boundary\n  JsonlWorld + CursorContext -> Suggestion[] with compileDraft -> queue.create JSONL\n\n");

    for (title, buffer, query) in cases {
        out.push_str(title);
        out.push('\n');
        out.push_str(&format!("> {buffer}\n"));
        for item in suggest(buffer, query) {
            out.push_str(&format!(
                "  {:<10} {:<10} score={:<4} compileDraft.op={}\n",
                item.label, item.kind, item.score, item.compile_op
            ));
        }
        out.push('\n');
    }

    if let Some(first) = suggest("{", None).first() {
        out.push_str("queue.create example\n");
        out.push_str(&queue_create_json(first));
        out.push('\n');
    }

    out
}

fn suggest(buffer: &str, query: Option<&str>) -> Vec<Suggestion> {
    let context = derive_cursor_context(buffer);
    let needle = query.unwrap_or(context.partial.as_str());
    let mut items = match context.state {
        CursorState::ObjectOpen | CursorState::Key | CursorState::NextKey => suggest_keys(&context, needle),
        CursorState::Value => suggest_values(&context, needle),
        CursorState::Other => Vec::new(),
    };
    items.sort_by(|a, b| b.score.cmp(&a.score).then_with(|| a.label.cmp(&b.label)));
    items
}

fn suggest_keys(context: &CursorContext, needle: &str) -> Vec<Suggestion> {
    let mut items = Vec::new();
    for prop in PROPERTIES {
        if context.present_keys.iter().any(|key| key == prop.key) {
            continue;
        }
        let Some(mut score) = fuzzy_score(needle, prop.key) else {
            continue;
        };
        if prop.required {
            score += 20;
        }
        let quoted = format!("\"{}\": ", prop.key);
        items.push(Suggestion {
            label: prop.key.to_string(),
            kind: "key",
            detail: prop.detail.to_string(),
            score,
            replace_partial: context.partial.clone(),
            insert_text: quoted,
            compile_op: if context.partial.is_empty() { "set_key" } else { "complete_key" },
            key: Some(prop.key.to_string()),
            value: None,
        });
    }
    items
}

fn suggest_values(context: &CursorContext, needle: &str) -> Vec<Suggestion> {
    let Some(current_key) = &context.current_key else {
        return Vec::new();
    };
    let Some(prop) = PROPERTIES.iter().find(|prop| prop.key == current_key) else {
        return Vec::new();
    };
    prop.values
        .iter()
        .filter_map(|value| {
            let score = fuzzy_score(needle, value)?;
            Some(Suggestion {
                label: (*value).to_string(),
                kind: "value",
                detail: format!("{} enum value", prop.key),
                score,
                replace_partial: context.partial.clone(),
                insert_text: (*value).to_string(),
                compile_op: "set_value",
                key: Some(prop.key.to_string()),
                value: Some((*value).to_string()),
            })
        })
        .collect()
}

fn derive_cursor_context(buffer: &str) -> CursorContext {
    let trimmed = buffer.trim_end();
    let present_keys = present_keys(buffer);

    if trimmed.ends_with('{') {
        return CursorContext {
            state: CursorState::ObjectOpen,
            partial: String::new(),
            present_keys,
            current_key: None,
        };
    }

    if trimmed.ends_with(',') {
        return CursorContext {
            state: CursorState::NextKey,
            partial: String::new(),
            present_keys,
            current_key: None,
        };
    }

    if let Some(colon) = trimmed.rfind(':') {
        let last_comma = trimmed.rfind(',').unwrap_or(0);
        if colon > last_comma {
            let current_key = key_before_colon(&trimmed[..colon]);
            let after_colon = &trimmed[colon + 1..];
            let partial = value_partial(after_colon);
            return CursorContext {
                state: CursorState::Value,
                partial,
                present_keys,
                current_key,
            };
        }
    }

    if let Some(partial) = key_partial(trimmed) {
        return CursorContext {
            state: CursorState::Key,
            partial,
            present_keys,
            current_key: None,
        };
    }

    CursorContext {
        state: CursorState::Other,
        partial: String::new(),
        present_keys,
        current_key: None,
    }
}

fn present_keys(buffer: &str) -> Vec<String> {
    let mut keys = Vec::new();
    let bytes = buffer.as_bytes();
    let mut i = 0;
    while i < bytes.len() {
        if bytes[i] != b'\"' {
            i += 1;
            continue;
        }
        let start = i + 1;
        let Some(end_rel) = buffer[start..].find('"') else {
            break;
        };
        let end = start + end_rel;
        let key = &buffer[start..end];
        let mut j = end + 1;
        while j < bytes.len() && bytes[j].is_ascii_whitespace() {
            j += 1;
        }
        if j < bytes.len() && bytes[j] == b':' && !keys.iter().any(|item| item == key) {
            keys.push(key.to_string());
        }
        i = end + 1;
    }
    keys
}

fn key_before_colon(left: &str) -> Option<String> {
    let end = left.rfind('"')?;
    let start = left[..end].rfind('"')? + 1;
    Some(left[start..end].to_string())
}

fn value_partial(after_colon: &str) -> String {
    let trimmed = after_colon.trim_start();
    if let Some(stripped) = trimmed.strip_prefix('"') {
        stripped.trim_end_matches('"').to_string()
    } else {
        trimmed.to_string()
    }
}

fn key_partial(trimmed: &str) -> Option<String> {
    let last_quote = trimmed.rfind('"')?;
    let after = &trimmed[last_quote + 1..];
    if after.contains(':') || after.contains(',') || after.contains('}') {
        return None;
    }
    Some(after.to_string())
}

fn fuzzy_score(needle: &str, haystack: &str) -> Option<i32> {
    if needle.is_empty() {
        return Some(1_000 - haystack.len() as i32);
    }
    let mut score = 0;
    let mut last_match: Option<usize> = None;
    let mut search_from = 0;
    let hay = haystack.to_ascii_lowercase();
    let nee = needle.to_ascii_lowercase();

    for ch in nee.chars() {
        let found = hay[search_from..].find(ch)? + search_from;
        score += 100;
        if let Some(prev) = last_match {
            if found == prev + 1 {
                score += 60;
            } else {
                score -= (found - prev) as i32;
            }
        } else {
            score -= found as i32;
        }
        last_match = Some(found);
        search_from = found + ch.len_utf8();
    }

    Some(score - haystack.len() as i32)
}

fn candidate_json(item: &Suggestion) -> String {
    let draft = compile_draft_json(item);
    format!(
        "{{\"type\":\"candidate.item\",\"label\":\"{}\",\"kind\":\"{}\",\"detail\":\"{}\",\"score\":{},\"edit\":{{\"replacePartial\":\"{}\",\"text\":\"{}\"}},\"compileDraft\":{}}}",
        json_escape(&item.label),
        item.kind,
        json_escape(&item.detail),
        item.score,
        json_escape(&item.replace_partial),
        json_escape(&item.insert_text),
        draft
    )
}

fn queue_create_json(item: &Suggestion) -> String {
    format!(
        "{{\"type\":\"queue.create\",\"item\":{{\"label\":\"{}\",\"kind\":\"{}\",\"compileDraft\":{}}}}}",
        json_escape(&item.label),
        item.kind,
        compile_draft_json(item)
    )
}

fn compile_draft_json(item: &Suggestion) -> String {
    match (&item.key, &item.value) {
        (Some(key), Some(value)) => format!(
            "{{\"op\":\"{}\",\"key\":\"{}\",\"value\":\"{}\"}}",
            item.compile_op,
            json_escape(key),
            json_escape(value)
        ),
        (Some(key), None) => format!(
            "{{\"op\":\"{}\",\"key\":\"{}\"}}",
            item.compile_op,
            json_escape(key)
        ),
        _ => format!("{{\"op\":\"{}\"}}", item.compile_op),
    }
}

fn json_escape(value: &str) -> String {
    value
        .replace('\\', "\\\\")
        .replace('"', "\\\"")
        .replace('\n', "\\n")
        .replace('\r', "\\r")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn fuzzy_matches_subsequence() {
        assert!(fuzzy_score("aex", "archive.extract").is_some());
        assert!(fuzzy_score("zx", "archive.extract").is_none());
    }

    #[test]
    fn suggests_required_key_at_object_open() {
        let items = suggest("{", None);
        assert_eq!(items[0].label, "status");
        assert_eq!(items[0].compile_op, "set_key");
    }

    #[test]
    fn suggests_enum_values() {
        let labels: Vec<String> = suggest("{\"status\": \"d", None)
            .into_iter()
            .map(|item| item.label)
            .collect();
        assert_eq!(labels, vec!["done", "draft", "deferred"]);
    }

    #[test]
    fn accept_creates_queue_jsonl() {
        let first = suggest("{", None).remove(0);
        let row = queue_create_json(&first);
        assert!(row.contains("\"type\":\"queue.create\""));
        assert!(row.contains("\"compileDraft\""));
    }
}
