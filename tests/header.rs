use agent_wrap::{
    header::{self, State},
    project::Context,
};
use ratatui::{buffer::Buffer, layout::Rect, style::Color};
fn context(id: &str) -> Context {
    Context {
        identity: id.into(),
        name: id.into(),
        dir: "/repo".into(),
        branch: "main".into(),
        default_branch: "main".into(),
        ..Default::default()
    }
}
#[test]
fn history_is_immutable_rewinds_and_retains_original_pin_on_narrow_terminals() {
    let initial = State::new(context("first"));
    let second = initial.moved(context("second"));
    let third = second.moved(context("third"));
    assert_eq!(initial.trail.len(), 1);
    assert_eq!(third.trail.len(), 3);
    assert_eq!(third.moved(context("first")).trail.len(), 1);
    for width in [0, 1, 2, 3, 4, 7, 9, 10, 60] {
        let mut buf = Buffer::empty(Rect::new(2, 1, width, 3));
        header::draw(&mut buf, &third, "Codex");
        if width >= 4 {
            assert_eq!(buf[(2, 1)].bg, header::bright("first"));
        }
        for cell in &buf.content {
            assert_eq!(cell.fg, Color::Black);
        }
    }
}
#[test]
fn context_colors_remain_bright_and_perceptually_distinct() {
    for n in 0..360 {
        let mut ctx = context(&format!("project-{n}"));
        let base = header::bright(&ctx.identity);
        assert_eq!(base, header::bright(&ctx.identity));
        for variant in 0..4 {
            ctx.subtree = if variant == 0 {
                "child".into()
            } else {
                String::new()
            };
            ctx.worktree = variant == 1;
            ctx.branch = if variant == 2 {
                "feature".into()
            } else {
                "main".into()
            };
            ctx.default_branch = if variant == 3 {
                String::new()
            } else {
                "main".into()
            };
            let color = header::background(&ctx);
            let Color::Rgb(r, g, b) = color else { panic!() };
            assert_eq!(r.max(g).max(b), 255);
            assert!(r.min(g).min(b) >= 80);
            assert!(header::distance(base, color) >= 30.0, "{n} {variant}");
        }
    }
}
#[test]
fn drawing_preserves_wide_text_strips_terminal_injection_and_labels_context() {
    let mut ctx = context("name\x1b[31m\nrepo");
    ctx.branch.clear();
    let mut buf = Buffer::empty(Rect::new(0, 0, 70, 3));
    header::draw(
        &mut buf,
        &State::new(ctx.clone()),
        "\x1b[31m審査e\u{301}\ragent",
    );
    assert_eq!(buf[(1, 0)].symbol(), "審");
    assert_eq!(buf[(3, 0)].symbol(), "査");
    assert_eq!(buf[(5, 0)].symbol(), "e\u{301}");
    let text = buf.content.iter().map(|c| c.symbol()).collect::<String>();
    assert!(text.contains("no Git repository"));
    assert!(!text.contains('\x1b'));
    for cell in &buf.content {
        assert_eq!(cell.fg, Color::Black);
    }
    ctx.branch = "feature".into();
    ctx.default_branch.clear();
    ctx.worktree = true;
    header::draw(&mut buf, &State::new(ctx), "Pi");
    let text = buf.content.iter().map(|c| c.symbol()).collect::<String>();
    assert!(text.contains("default branch unknown · worktree"));
}
