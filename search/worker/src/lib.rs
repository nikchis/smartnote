//! The search worker `smartnote-search`: a child process of Smartnote that indexes notes
//! and answers search queries over stdin/stdout (spec §6).

pub mod embed;
pub mod engine;
pub mod harden;
pub mod index;
pub mod panic;
pub mod protocol;
pub mod rank;
pub mod sandbox;
pub mod text;
pub mod worker;

pub use worker::run;
