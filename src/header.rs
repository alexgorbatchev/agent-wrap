use crate::project::Context;
use palette::{FromColor, Hsv, Lab, Srgb, color_difference::Ciede2000};
use ratatui::{
    buffer::Buffer,
    layout::Rect,
    style::{Color, Style},
};
use sha2::{Digest, Sha256};
use std::ffi::OsStr;

pub const ROWS: u16 = 3;
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct State {
    pub current: Context,
    pub trail: Vec<Context>,
}
impl State {
    pub fn new(current: Context) -> Self {
        Self {
            trail: vec![current.clone()],
            current,
        }
    }
    pub fn moved(&self, next: Context) -> Self {
        let mut trail = self.trail.clone();
        if let Some(index) = trail.iter().position(|p| p.identity == next.identity) {
            trail.truncate(index + 1);
            trail[index] = next.clone();
        } else {
            trail.push(next.clone());
        }
        Self {
            current: next,
            trail,
        }
    }
}
fn hue(key: &[u8]) -> f64 {
    let sum = Sha256::digest(key);
    (u64::from_be_bytes(sum[..8].try_into().expect("SHA-256 prefix")) % 36_000) as f64 / 100.0
}
fn hue_color(h: f64) -> Color {
    let rgb: Srgb<f64> = Srgb::from_color(Hsv::new(h, 0.65, 1.0));
    let rgb: Srgb<u8> = rgb.into_format();
    Color::Rgb(rgb.red, rgb.green, rgb.blue)
}
pub fn bright(key: impl AsRef<OsStr>) -> Color {
    hue_color(hue(key.as_ref().as_encoded_bytes()))
}
pub fn distance(left: Color, right: Color) -> f64 {
    let convert = |color| {
        let Color::Rgb(r, g, b) = color else {
            unreachable!("generated RGB palette")
        };
        Lab::<palette::white_point::D65, f64>::from_color(
            Srgb::<u8>::new(r, g, b).into_format::<f64>(),
        )
    };
    convert(left).difference(convert(right))
}
pub fn background(context: &Context) -> Color {
    let base = bright(&context.identity);
    if !context.split() {
        return base;
    }
    let identity = context.identity.as_encoded_bytes();
    let key = [
        identity,
        b"\0",
        context.branch.as_encoded_bytes(),
        b"\0",
        context.subtree.as_encoded_bytes(),
        if context.worktree { b"\0worktree" } else { b"" },
    ]
    .concat();
    let start = hue(identity) + 60.0 + hue(&key) % 240.0;
    let (mut best, mut farthest) = (base, 0.0);
    for i in 0..12 {
        let candidate = hue_color((start + f64::from(i) * 30.0) % 360.0);
        let delta = distance(base, candidate);
        if delta >= 30.0 {
            return candidate;
        }
        if delta > farthest {
            best = candidate;
            farthest = delta;
        }
    }
    best
}
fn safe_text(text: &str) -> String {
    strip_ansi_escapes::strip_str(text)
        .chars()
        .map(|c| if c.is_control() { ' ' } else { c })
        .collect()
}
pub fn draw(view: &mut Buffer, state: &State, name: &str) {
    let bounds = view.area;
    if bounds.is_empty() {
        return;
    }
    let style = Style::default()
        .fg(Color::Black)
        .bg(background(&state.current));
    let capacity = usize::from(bounds.width.saturating_sub(1) / 3);
    let count = state.trail.len().saturating_sub(1) + usize::from(state.current.split());
    let visible = if count > capacity {
        capacity.min(1)
    } else {
        count
    };
    let start = bounds.x + u16::try_from(visible).expect("bounded pins") * 3;
    let width = bounds.right().saturating_sub(start + 1);
    let mut branch = if state.current.branch.is_empty() {
        "no Git repository".into()
    } else if state.current.default_branch.is_empty() {
        format!(
            "{} · default branch unknown",
            state.current.branch.to_string_lossy()
        )
    } else {
        state.current.branch.to_string_lossy().into_owned()
    };
    if state.current.worktree {
        branch.push_str(" · worktree");
    }
    for (row, text) in [
        name.to_owned(),
        format!(
            "{} · {}",
            state.current.name,
            cli_command_kit::display_path(&state.current.dir)
        ),
        branch,
    ]
    .into_iter()
    .take(usize::from(bounds.height.min(ROWS)))
    .enumerate()
    {
        if width > 0 {
            view.set_stringn(
                start + 1,
                bounds.y + u16::try_from(row).expect("three rows"),
                safe_text(&text),
                usize::from(width),
                style,
            );
        }
    }
    // Ratatui resets wide-character continuation cells while writing text. Its
    // style-only operation recolors them without replacing any grapheme.
    view.set_style(bounds, style);
    for (i, context) in state.trail.iter().take(visible).enumerate() {
        view.set_style(
            Rect::new(
                bounds.x + u16::try_from(i).expect("bounded pins") * 3,
                bounds.y,
                3,
                bounds.height,
            ),
            style.bg(bright(&context.identity)),
        );
    }
}
