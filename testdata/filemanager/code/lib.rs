/// Parses a `key=value` line, ignoring comments.
pub fn parse_line(line: &str) -> Option<(&str, &str)> {
    let line = line.trim();
    if line.is_empty() || line.starts_with('#') {
        return None;
    }
    line.split_once('=').map(|(k, v)| (k.trim(), v.trim()))
}
