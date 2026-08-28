<?php
header('Content-Type: application/json');

$upload = null;
if (isset($_FILES['attachment'])) {
    $upload = [
        'name' => $_FILES['attachment']['name'],
        'contents' => file_get_contents($_FILES['attachment']['tmp_name']),
    ];
}

echo json_encode([
    'get' => $_GET['name'] ?? null,
    'post' => $_POST['message'] ?? null,
    'upload' => $upload,
    'path_info' => $_SERVER['PATH_INFO'] ?? null,
]);
