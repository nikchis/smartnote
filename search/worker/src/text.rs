//! Text processing shared by indexing and queries (spec §6.3, §6.4).

pub mod form;
pub mod query;

pub use form::{NORMALIZATION_VERSION, SearchForm, search_form};
pub use query::{Query, parse_query};
