cmake_minimum_required(VERSION 3.22)

if(NOT TARGET UE4SS)
  message(FATAL_ERROR "PalPanelBridge must be added from the RE-UE4SS root CMake project")
endif()

add_library(PalPanelBridge SHARED src/main.cpp)
target_compile_features(PalPanelBridge PRIVATE cxx_std_23)
target_compile_definitions(PalPanelBridge PRIVATE WIN32_LEAN_AND_MEAN NOMINMAX)
target_link_libraries(PalPanelBridge PRIVATE UE4SS ws2_32)
set_target_properties(PalPanelBridge PROPERTIES
  OUTPUT_NAME "main"
  RUNTIME_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/artifact/PalPanelBridge/dlls"
  RUNTIME_OUTPUT_DIRECTORY_RELEASE "${CMAKE_BINARY_DIR}/artifact/PalPanelBridge/dlls"
  RUNTIME_OUTPUT_DIRECTORY_GAME__SHIPPING__WIN64 "${CMAKE_BINARY_DIR}/artifact/PalPanelBridge/dlls"
)
