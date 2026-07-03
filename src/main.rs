use std::env;
use std::process;

const DEMO_AUTOCOMPLETE: &str = r#"hq rust proposal build

status
  proposal-only

surface
  cmp-like matcher repl path

boundary
  JsonlWorld + CursorContext
    -> Suggestion[] with compileDraft
    -> matcher ranks Suggestion[]
    -> cmp-like surface displays top suggestions
    -> accept creates queue instruction JSONL

artifact
  hq.exe

note
  This binary proves the Rust Windows release path only.
  It does not claim that runtime REPL completion is complete.
"#;

fn main() {
    let mut args = env::args().skip(1);
    let command = args.next().unwrap_or_else(|| "demo-autocomplete".to_string());

    match command.as_str() {
        "demo-autocomplete" => {
            print!("{DEMO_AUTOCOMPLETE}");
        }
        "version" | "--version" | "-V" => {
            println!("hq 0.0.0 proposal-rust-build");
        }
        "help" | "--help" | "-h" => {
            println!("usage: hq [demo-autocomplete|version]");
        }
        other => {
            eprintln!("unknown command: {other}");
            eprintln!("usage: hq [demo-autocomplete|version]");
            process::exit(2);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::DEMO_AUTOCOMPLETE;

    #[test]
    fn demo_names_rust_artifact() {
        assert!(DEMO_AUTOCOMPLETE.contains("hq.exe"));
    }

    #[test]
    fn demo_does_not_claim_runtime_completion_done() {
        assert!(DEMO_AUTOCOMPLETE.contains("does not claim that runtime REPL completion is complete"));
    }
}
