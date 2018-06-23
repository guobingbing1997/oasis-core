//! Prometheus metric pusher.
use ekiden_common::environment::Environment;
use ekiden_common::error::{Error, Result};
use ekiden_common::futures::prelude::*;
use futures_timer::Interval;
use prometheus;
use std::net;
use std::sync::Arc;
use std::time::Duration;

/// Prometheus metrics endpoint.
fn push_metrics() -> Result<()> {
    // address: net::SocketAddr
    let encoder = prometheus::TextEncoder::new();
    let metric_familys = prometheus::gather();
    let address = "127.0.0.1:9091".to_owned();
    println!("Pushing metrics!");
    prometheus::push_metrics(
        "example_push",
        labels!{"instance".to_owned() => "HAL-9000".to_owned(),},
        &address,
        metric_familys,
    ).unwrap();
    Ok(())
}

/// Start an HTTP server for Prometheus metrics.
pub fn start(environment: Arc<Environment>, address: net::SocketAddr) {
    let push = Box::new(
        Interval::new(Duration::from_secs(5))
            .map_err(|error| Error::from(error))
            // On each tick of the interval, push metrics.
            .for_each(|_| {
                push_metrics()
            })
            .then(|_| future::ok(())),
    );

    info!("Starting Prometheus metrics pusher!");
    environment.spawn(push);
}
