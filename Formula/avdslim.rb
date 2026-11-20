class Avdslim < Formula
  desc "Cut Android emulator (AVD) host RAM from ~8.5 GB to ~2.5 GB on Apple Silicon & Linux"
  homepage "https://github.com/kdbhalala/avdslim"
  version "1.0.17"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/kdbhalala/avdslim/releases/download/v1.0.17/avdslim_v1.0.17_darwin_arm64.tar.gz"
      sha256 "4921896923988ce1205b6cc1bb370c46da9d46a6d76f5ac4fc27eca537fdcf6c"
    else
      url "https://github.com/kdbhalala/avdslim/releases/download/v1.0.17/avdslim_v1.0.17_darwin_amd64.tar.gz"
      sha256 "2ef39d81801cc8b4f0cd3e49afb16461f345e8b1abfbb20b95e7c3d78cce9185"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/kdbhalala/avdslim/releases/download/v1.0.17/avdslim_v1.0.17_linux_arm64.tar.gz"
      sha256 "79a46fa206bda6db67206337a3b78f503f8cd426e4b4b604b6bb26aaf46ed4e4"
    else
      url "https://github.com/kdbhalala/avdslim/releases/download/v1.0.17/avdslim_v1.0.17_linux_amd64.tar.gz"
      sha256 "bb4adaec3b33a084c134162a384b3687696b8c98ad92eeb8ba0ccd841f345569"
    end
  end

  def install
    bin.install "avdslim"
  end

  test do
    assert_match "avdslim", shell_output("#{bin}/avdslim version")
  end
end
