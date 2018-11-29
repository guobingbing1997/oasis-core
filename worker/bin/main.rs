#[macro_use]
extern crate clap;
extern crate log;
extern crate pretty_env_logger;
extern crate tokio_uds;

extern crate ekiden_core;
extern crate ekiden_di;
extern crate ekiden_instrumentation;
extern crate ekiden_instrumentation_prometheus;
extern crate ekiden_storage_multilayer;
extern crate ekiden_storage_persistent;
extern crate ekiden_tracing;
extern crate ekiden_worker;
extern crate ekiden_worker_api;

use std::path::Path;
use std::sync::Arc;

use clap::{App, Arg};
use log::{info, LevelFilter};

use ekiden_core::environment::Environment;
use ekiden_core::futures::block_on;
use ekiden_di::{Component, KnownComponents};
use ekiden_instrumentation::{set_boxed_metric_collector, MetricCollector};
use ekiden_storage_multilayer::MultilayerBackend;
use ekiden_storage_persistent::PersistentStorageBackend;
use ekiden_worker_api::WorkerHandler;

use ekiden_worker::protocol::ProtocolHandler;
use ekiden_worker::worker::{Worker, WorkerConfiguration};

/// Register known components for dependency injection.
fn register_components(known_components: &mut KnownComponents) {
    // Environment.
    ekiden_core::environment::GrpcEnvironment::register(known_components);
    // Instrumentation.
    ekiden_instrumentation_prometheus::PrometheusMetricCollector::register(known_components);
}

fn main() {
    // Create known components registry.
    let mut known_components = KnownComponents::new();
    register_components(&mut known_components);

    let matches = App::new("Ekiden worker process")
        .arg(
            Arg::with_name("runtime")
                .index(1)
                .value_name("RUNTIME")
                .help("Signed runtime filename")
                .takes_value(true)
                .required(true)
                .display_order(1)
                .index(1),
        )
        .arg(
            Arg::with_name("host-socket")
                .long("host-socket")
                .takes_value(true)
                .display_order(2)
                .required(true),
        )
        .arg(
            Arg::with_name("cache-dir")
                .long("cache-dir")
                .takes_value(true)
                .display_order(3)
                .required(true),
        )
        .args(&known_components.get_arguments())
        .args(&ekiden_tracing::get_arguments())
        .get_matches();

    // Initialize logger.
    pretty_env_logger::formatted_builder()
        .unwrap()
        .filter(None, LevelFilter::Trace)
        .filter(Some("mio"), LevelFilter::Warn)
        .filter(Some("tokio_threadpool"), LevelFilter::Warn)
        .filter(Some("tokio_reactor"), LevelFilter::Warn)
        .filter(Some("tokio_io"), LevelFilter::Warn)
        .filter(Some("tokio_core"), LevelFilter::Warn)
        .filter(Some("hyper"), LevelFilter::Warn)
        .filter(Some("mime"), LevelFilter::Warn)
        .filter(Some("pagecache::io"), LevelFilter::Debug)
        .filter(Some("want"), LevelFilter::Debug)
        .init();

    // Initialize component container.
    let mut container = known_components
        .build_with_arguments(&matches)
        .expect("failed to initialize component container");

    // Initialize metric collector.
    let metrics = container
        .inject_owned::<MetricCollector>()
        .expect("failed to inject MetricCollector");
    set_boxed_metric_collector(metrics).unwrap();

    // Initialize tracing.
    ekiden_tracing::report_forever("ekiden-worker", &matches);

    // Check if passed runtime exists.
    let runtime_filename = matches.value_of("runtime").unwrap().to_owned();
    if !Path::new(&runtime_filename).exists() {
        panic!("Could not find runtime: {}", runtime_filename);
    }

    let environment = container.inject::<Environment>().unwrap();

    // Connect to passed UNIX socket.
    let socket = block_on(
        environment.clone(),
        tokio_uds::UnixStream::connect(
            value_t!(matches, "host-socket", String).unwrap_or_else(|e| e.exit()),
        ),
    ).expect("connect to host must succeed");

    // Create protocol instance.
    let protocol_handler = Arc::new(WorkerHandler(ProtocolHandler::new()));
    let (protocol, shutdown_signal) =
        ekiden_worker_api::Protocol::new(environment.clone(), socket, protocol_handler.clone());
    let protocol = Arc::new(protocol);

    // Create storage backend.
    let storage_backend = Arc::new(MultilayerBackend::new(
        // Use per-worker cache directory.
        Arc::new(
            PersistentStorageBackend::new(&Path::new(matches.value_of("cache-dir").unwrap()).join("storage"))
                .expect("persistent storage backend creation must succeed"),
        ),
        // Use protocol for the bottom storage layer.
        protocol.clone(),
    ));

    // Setup worker thread.
    let worker = Worker::new(
        WorkerConfiguration {
            runtime_filename,
            // TODO: Consider saving/loading identity via the protocol.
            saved_identity_path: None,
        },
        // Use protocol for IAS.
        protocol.clone(),
        storage_backend,
    );
    protocol_handler.0.set_worker(worker);

    // Wait for the shutdown signal.
    block_on(environment, shutdown_signal).unwrap();

    info!("Node has terminated, shutting down");
}